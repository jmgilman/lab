// Package hooks provides hook execution for the image pipeline.
package hooks

import (
	"context"
	"fmt"
	"os/exec"
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
	client store.Client
}

// NewExecutor creates a new hook executor.
// If client is nil, caching is disabled.
func NewExecutor(client store.Client) *Executor {
	return &Executor{client: client}
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

	start := time.Now()
	output, err := cmd.CombinedOutput()
	duration := time.Since(start)

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
