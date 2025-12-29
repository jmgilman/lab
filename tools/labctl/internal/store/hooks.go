// Package store provides storage operations for the image pipeline.
package store

import (
	"path"
	"time"
)

// HookResult represents the cached result of a hook execution.
type HookResult struct {
	// HookName is the name of the hook that was executed.
	HookName string `json:"hookName"`
	// Checksum is the image checksum when the test was run.
	Checksum string `json:"checksum"`
	// Passed indicates whether the hook execution succeeded.
	Passed bool `json:"passed"`
	// ExecutedAt is when the hook was executed.
	ExecutedAt time.Time `json:"executedAt"`
	// Duration is how long the hook took to execute.
	Duration string `json:"duration"`
	// Output contains the first 10KB of combined stdout/stderr for debugging.
	Output string `json:"output,omitempty"`
}

// HookResultKey returns the S3 key for storing hook results.
// Example: "hooks/vyos/vyos-2025.11.iso/vyos-integration-test.json"
func HookResultKey(imagePath, hookName string) string {
	return path.Join("hooks", imagePath, hookName+".json")
}
