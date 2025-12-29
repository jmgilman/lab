// Package hooks provides hook execution for the image pipeline.
package hooks

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/GilmanLab/lab/tools/labctl/internal/config"
	"github.com/GilmanLab/lab/tools/labctl/internal/store"
)

const (
	// DefaultTimeout is the default timeout for hook execution.
	DefaultTimeout = 30 * time.Minute
	// MaxOutputSize is the maximum size of hook output to store (10KB).
	MaxOutputSize = 10 * 1024
)

// Executor runs hooks and manages result caching.
type Executor struct {
	client   store.Client
	cacheDir string
}

// NewExecutor creates a new hook executor.
// If client is nil, S3 result caching is disabled.
// If cacheDir is non-empty, it will be passed to hooks as LABCTL_HOOK_CACHE.
func NewExecutor(client store.Client, cacheDir string) *Executor {
	return &Executor{client: client, cacheDir: cacheDir}
}

// TransformResult contains the result of running transform hooks.
type TransformResult struct {
	// OutputPath is the path to the transformed file.
	// If no transform hooks were run, this is empty.
	OutputPath string
	// Cleanup should be called to remove any temporary files created.
	// Safe to call even if OutputPath is empty.
	Cleanup func()
}

// RunTransformHooks executes all transform hooks for an image.
// Each hook receives a copy of the file and can modify it in-place.
// The hooks are chained: each hook receives the output of the previous hook.
// Returns the path to the final transformed file, or empty string if no hooks.
// The caller must call Cleanup() when done with the transformed file.
func (e *Executor) RunTransformHooks(ctx context.Context, img config.Image, imagePath string) (*TransformResult, error) {
	if img.Hooks == nil || len(img.Hooks.Transform) == 0 {
		return &TransformResult{Cleanup: func() {}}, nil
	}

	// Create a working copy of the file for transformations
	workFile, err := copyToTemp(imagePath)
	if err != nil {
		return nil, fmt.Errorf("create working copy: %w", err)
	}

	cleanup := func() {
		_ = os.Remove(workFile)
	}

	// Run each transform hook in sequence
	for _, hook := range img.Hooks.Transform {
		if err := e.runTransformHook(ctx, hook, workFile); err != nil {
			cleanup()
			return nil, fmt.Errorf("transform hook %q failed: %w", hook.Name, err)
		}
	}

	return &TransformResult{
		OutputPath: workFile,
		Cleanup:    cleanup,
	}, nil
}

// runTransformHook executes a single transform hook.
// The hook modifies the file at workPath in-place.
func (e *Executor) runTransformHook(ctx context.Context, hook config.Hook, workPath string) error {
	// Parse timeout
	timeout := DefaultTimeout
	if hook.Timeout != "" {
		var err error
		timeout, err = time.ParseDuration(hook.Timeout)
		if err != nil {
			return fmt.Errorf("invalid timeout %q: %w", hook.Timeout, err)
		}
	}

	// Execute hook
	fmt.Printf("  Running transform hook %q...\n", hook.Name)
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := append([]string{workPath}, hook.Args...)
	cmd := exec.CommandContext(hookCtx, hook.Command, args...) //nolint:gosec // G204: Command is from trusted manifest
	if hook.WorkDir != "" {
		cmd.Dir = hook.WorkDir
	}

	// Set up hook cache directory if configured
	if e.cacheDir != "" {
		safeName := sanitizeHookName(hook.Name)
		hookCacheDir := filepath.Join(e.cacheDir, "hooks", safeName)
		if err := os.MkdirAll(hookCacheDir, 0o750); err != nil {
			fmt.Printf("  Warning: failed to create hook cache dir: %v\n", err)
		} else {
			cmd.Env = append(os.Environ(), "LABCTL_HOOK_CACHE="+hookCacheDir)
		}
	}

	// Set up output streaming with prefix
	var outputBuf bytes.Buffer
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start hook: %w", err)
	}

	// Stream output with prefix
	var wg sync.WaitGroup
	wg.Add(2)
	go streamWithPrefix(&wg, stdout, &outputBuf, "  │ ")
	go streamWithPrefix(&wg, stderr, &outputBuf, "  │ ")
	wg.Wait()

	err = cmd.Wait()
	duration := time.Since(start)
	output := outputBuf.Bytes()

	if err != nil {
		return fmt.Errorf("exit status %v:\n%s", err, truncateOutput(string(output), 1024))
	}

	fmt.Printf("  Transform hook %q: completed (%s)\n", hook.Name, duration.Round(time.Second))
	return nil
}

// copyToTemp creates a temporary copy of the file at srcPath.
// Returns the path to the temporary file.
func copyToTemp(srcPath string) (string, error) {
	src, err := os.Open(srcPath) //nolint:gosec // G304: Path is from trusted internal source
	if err != nil {
		return "", fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.CreateTemp("", "labctl-transform-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	dstPath := dst.Name()

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(dstPath)
		return "", fmt.Errorf("copy file: %w", err)
	}

	if err := dst.Close(); err != nil {
		_ = os.Remove(dstPath)
		return "", fmt.Errorf("close temp file: %w", err)
	}

	return dstPath, nil
}

// RunPreUploadHooks executes all pre-upload hooks for an image.
// Returns nil if all hooks pass, error if any hook fails.
func (e *Executor) RunPreUploadHooks(ctx context.Context, img config.Image, imagePath, checksum string) error {
	if img.Hooks == nil || len(img.Hooks.PreUpload) == 0 {
		return nil
	}

	for _, hook := range img.Hooks.PreUpload {
		if err := e.runHook(ctx, img.Destination, hook, imagePath, checksum); err != nil {
			return fmt.Errorf("hook %q failed: %w", hook.Name, err)
		}
	}
	return nil
}

func (e *Executor) runHook(ctx context.Context, destination string, hook config.Hook, imagePath, checksum string) error {
	// Check cache first
	if e.client != nil {
		cached, err := e.client.GetHookResult(ctx, destination, hook.Name)
		if err != nil {
			// Log but continue - cache errors shouldn't block execution
			fmt.Printf("  Warning: failed to check hook cache: %v\n", err)
		} else if cached != nil && cached.Checksum == checksum && cached.Passed {
			fmt.Printf("  Hook %q: cached pass (tested %s)\n", hook.Name, cached.ExecutedAt.Format(time.RFC3339))
			return nil
		}
	}

	// Parse timeout
	timeout := DefaultTimeout
	if hook.Timeout != "" {
		var err error
		timeout, err = time.ParseDuration(hook.Timeout)
		if err != nil {
			return fmt.Errorf("invalid timeout %q: %w", hook.Timeout, err)
		}
	}

	// Execute hook
	fmt.Printf("  Running hook %q...\n", hook.Name)
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := append([]string{imagePath}, hook.Args...)
	cmd := exec.CommandContext(hookCtx, hook.Command, args...) //nolint:gosec // G204: Command is from trusted manifest
	if hook.WorkDir != "" {
		cmd.Dir = hook.WorkDir
	}

	// Set up hook cache directory if configured
	if e.cacheDir != "" {
		// Sanitize hook name for filesystem safety
		safeName := sanitizeHookName(hook.Name)
		hookCacheDir := filepath.Join(e.cacheDir, "hooks", safeName)
		if err := os.MkdirAll(hookCacheDir, 0o750); err != nil {
			fmt.Printf("  Warning: failed to create hook cache dir: %v\n", err)
		} else {
			cmd.Env = append(os.Environ(), "LABCTL_HOOK_CACHE="+hookCacheDir)
		}
	}

	// Set up output streaming with prefix
	var outputBuf bytes.Buffer
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start hook: %w", err)
	}

	// Stream output with prefix
	var wg sync.WaitGroup
	wg.Add(2)
	go streamWithPrefix(&wg, stdout, &outputBuf, "  │ ")
	go streamWithPrefix(&wg, stderr, &outputBuf, "  │ ")
	wg.Wait()

	err = cmd.Wait()
	duration := time.Since(start)
	output := outputBuf.Bytes()

	// Store result
	result := &store.HookResult{
		HookName:   hook.Name,
		Checksum:   checksum,
		Passed:     err == nil,
		ExecutedAt: start,
		Duration:   duration.String(),
		Output:     truncateOutput(string(output), MaxOutputSize),
	}

	if e.client != nil {
		if cacheErr := e.client.PutHookResult(ctx, destination, hook.Name, result); cacheErr != nil {
			fmt.Printf("  Warning: failed to cache hook result: %v\n", cacheErr)
		}
	}

	if err != nil {
		return fmt.Errorf("exit status %v:\n%s", err, truncateOutput(string(output), 1024))
	}

	fmt.Printf("  Hook %q: passed (%s)\n", hook.Name, duration.Round(time.Second))
	return nil
}

// truncateOutput truncates a string to maxLen bytes, adding a truncation notice if needed.
func truncateOutput(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... (truncated)"
}

// streamWithPrefix reads from r line by line, prints each line with a prefix to stdout,
// and writes the original content to the buffer for caching.
func streamWithPrefix(wg *sync.WaitGroup, r io.Reader, buf *bytes.Buffer, prefix string) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintln(os.Stdout, prefix+line)
		_, _ = buf.WriteString(line + "\n")
	}
}

// sanitizeHookName makes a hook name safe for use as a directory name.
func sanitizeHookName(name string) string {
	var result []byte
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result = append(result, c)
		} else {
			result = append(result, '_')
		}
	}
	return string(result)
}
