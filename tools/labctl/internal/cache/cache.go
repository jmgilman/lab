// Package cache provides local file caching for the image pipeline.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Manager handles local file caching by checksum.
// Files are stored in a directory structure under the base directory.
type Manager struct {
	baseDir string
}

// NewManager creates a new cache manager with the given base directory.
// Returns an error if the directory cannot be created.
func NewManager(baseDir string) (*Manager, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("cache directory cannot be empty")
	}

	// Create base directory and subdirectories
	dirs := []string{
		filepath.Join(baseDir, "downloads"),
		filepath.Join(baseDir, "hooks"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create cache directory %s: %w", dir, err)
		}
	}

	return &Manager{baseDir: baseDir}, nil
}

// BaseDir returns the base cache directory.
func (m *Manager) BaseDir() string {
	return m.baseDir
}

// Get returns the path to a cached file for the given checksum.
// Returns the path and true if the file exists, empty string and false otherwise.
func (m *Manager) Get(checksum string) (string, bool) {
	key := checksumKey(checksum)
	cachePath := filepath.Join(m.baseDir, "downloads", key)

	if _, err := os.Stat(cachePath); err != nil {
		return "", false
	}

	return cachePath, true
}

// Put stores content from the reader to the cache under the given checksum.
// Returns the path to the cached file.
func (m *Manager) Put(checksum string, src io.Reader) (string, error) {
	key := checksumKey(checksum)
	cachePath := filepath.Join(m.baseDir, "downloads", key)

	// Write to temp file first, then rename for atomicity
	tempPath := cachePath + ".tmp"
	f, err := os.Create(tempPath) //nolint:gosec // G304: Path is constructed from trusted cache directory
	if err != nil {
		return "", fmt.Errorf("create cache file: %w", err)
	}

	_, err = io.Copy(f, src)
	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("write cache file: %w", err)
	}

	if err := os.Rename(tempPath, cachePath); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("rename cache file: %w", err)
	}

	return cachePath, nil
}

// Remove deletes a cached file for the given checksum.
func (m *Manager) Remove(checksum string) error {
	key := checksumKey(checksum)
	cachePath := filepath.Join(m.baseDir, "downloads", key)
	return os.Remove(cachePath)
}

// HookDir returns the cache directory for a specific hook.
// Creates the directory if it doesn't exist.
func (m *Manager) HookDir(hookName string) (string, error) {
	// Sanitize hook name for filesystem safety
	safeName := sanitizeName(hookName)
	hookDir := filepath.Join(m.baseDir, "hooks", safeName)

	if err := os.MkdirAll(hookDir, 0o750); err != nil {
		return "", fmt.Errorf("create hook cache directory: %w", err)
	}

	return hookDir, nil
}

// checksumKey converts a checksum string to a safe cache key.
// Uses the first 12 characters of sha256 hash for compact, unique names.
func checksumKey(checksum string) string {
	// Remove algorithm prefix if present (e.g., "sha256:abc123...")
	if idx := strings.Index(checksum, ":"); idx != -1 {
		checksum = checksum[idx+1:]
	}

	// Hash the checksum to get a consistent, safe filename
	h := sha256.Sum256([]byte(checksum))
	return hex.EncodeToString(h[:])[:12]
}

// sanitizeName makes a string safe for use as a filename.
func sanitizeName(name string) string {
	// Replace any non-alphanumeric characters with underscore
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
		} else {
			result.WriteRune('_')
		}
	}
	return result.String()
}
