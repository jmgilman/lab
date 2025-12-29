package hooks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GilmanLab/lab/tools/labctl/internal/config"
)

func TestRunTransformHooks(t *testing.T) {
	t.Run("no hooks returns empty result", func(t *testing.T) {
		executor := NewExecutor(nil, "")
		img := config.Image{
			Name: "test-image",
		}

		result, err := executor.RunTransformHooks(context.Background(), img, "/nonexistent")

		require.NoError(t, err)
		assert.Empty(t, result.OutputPath)
		assert.NotNil(t, result.Cleanup)
		result.Cleanup() // Should not panic
	})

	t.Run("empty hooks returns empty result", func(t *testing.T) {
		executor := NewExecutor(nil, "")
		img := config.Image{
			Name:  "test-image",
			Hooks: &config.Hooks{},
		}

		result, err := executor.RunTransformHooks(context.Background(), img, "/nonexistent")

		require.NoError(t, err)
		assert.Empty(t, result.OutputPath)
	})

	t.Run("transform hook modifies file in-place", func(t *testing.T) {
		// Create a temp file with initial content
		dir := t.TempDir()
		inputFile := filepath.Join(dir, "input.txt")
		err := os.WriteFile(inputFile, []byte("original content"), 0o600)
		require.NoError(t, err)

		// Create a script that modifies the file passed as first argument
		scriptFile := filepath.Join(dir, "transform.sh")
		scriptContent := `#!/bin/sh
echo " transformed" >> "$1"
`
		err = os.WriteFile(scriptFile, []byte(scriptContent), 0o755) //nolint:gosec // G306: Script needs execute permission
		require.NoError(t, err)

		executor := NewExecutor(nil, "")
		img := config.Image{
			Name: "test-image",
			Hooks: &config.Hooks{
				Transform: []config.Hook{
					{
						Name:    "append-hook",
						Command: scriptFile,
					},
				},
			},
		}

		result, err := executor.RunTransformHooks(context.Background(), img, inputFile)
		require.NoError(t, err)
		require.NotEmpty(t, result.OutputPath)
		defer result.Cleanup()

		// Verify the transformed file has the expected content
		content, err := os.ReadFile(result.OutputPath) //nolint:gosec
		require.NoError(t, err)
		assert.Equal(t, "original content transformed\n", string(content))

		// Original file should be unchanged
		originalContent, err := os.ReadFile(inputFile) //nolint:gosec // G304: Test file path
		require.NoError(t, err)
		assert.Equal(t, "original content", string(originalContent))
	})

	t.Run("multiple transform hooks chain correctly", func(t *testing.T) {
		dir := t.TempDir()
		inputFile := filepath.Join(dir, "input.txt")
		err := os.WriteFile(inputFile, []byte("start"), 0o600)
		require.NoError(t, err)

		// Create scripts for each hook
		script1 := filepath.Join(dir, "first.sh")
		err = os.WriteFile(script1, []byte("#!/bin/sh\necho '-first' >> \"$1\"\n"), 0o755) //nolint:gosec // G306: Script needs execute permission
		require.NoError(t, err)

		script2 := filepath.Join(dir, "second.sh")
		err = os.WriteFile(script2, []byte("#!/bin/sh\necho '-second' >> \"$1\"\n"), 0o755) //nolint:gosec // G306: Script needs execute permission
		require.NoError(t, err)

		executor := NewExecutor(nil, "")
		img := config.Image{
			Name: "test-image",
			Hooks: &config.Hooks{
				Transform: []config.Hook{
					{
						Name:    "first-hook",
						Command: script1,
					},
					{
						Name:    "second-hook",
						Command: script2,
					},
				},
			},
		}

		result, err := executor.RunTransformHooks(context.Background(), img, inputFile)
		require.NoError(t, err)
		require.NotEmpty(t, result.OutputPath)
		defer result.Cleanup()

		content, err := os.ReadFile(result.OutputPath) //nolint:gosec
		require.NoError(t, err)
		assert.Equal(t, "start-first\n-second\n", string(content))
	})

	t.Run("hook failure returns error and cleans up", func(t *testing.T) {
		dir := t.TempDir()
		inputFile := filepath.Join(dir, "input.txt")
		err := os.WriteFile(inputFile, []byte("content"), 0o600)
		require.NoError(t, err)

		// Create a script that fails
		failScript := filepath.Join(dir, "fail.sh")
		err = os.WriteFile(failScript, []byte("#!/bin/sh\nexit 1\n"), 0o755) //nolint:gosec // G306: Script needs execute permission
		require.NoError(t, err)

		executor := NewExecutor(nil, "")
		img := config.Image{
			Name: "test-image",
			Hooks: &config.Hooks{
				Transform: []config.Hook{
					{
						Name:    "failing-hook",
						Command: failScript,
					},
				},
			},
		}

		result, err := executor.RunTransformHooks(context.Background(), img, inputFile)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failing-hook")
		assert.Nil(t, result)
	})

	t.Run("hook receives LABCTL_HOOK_CACHE when cacheDir configured", func(t *testing.T) {
		dir := t.TempDir()
		cacheDir := filepath.Join(dir, "cache")
		inputFile := filepath.Join(dir, "input.txt")
		outputFile := filepath.Join(dir, "env.txt")
		err := os.WriteFile(inputFile, []byte("content"), 0o600)
		require.NoError(t, err)

		// Create a script that writes the cache env var to a file
		checkScript := filepath.Join(dir, "check-env.sh")
		scriptContent := fmt.Sprintf("#!/bin/sh\necho $LABCTL_HOOK_CACHE > %s\n", outputFile)
		err = os.WriteFile(checkScript, []byte(scriptContent), 0o755) //nolint:gosec // G306: Script needs execute permission
		require.NoError(t, err)

		executor := NewExecutor(nil, cacheDir)
		img := config.Image{
			Name: "test-image",
			Hooks: &config.Hooks{
				Transform: []config.Hook{
					{
						Name:    "env-check",
						Command: checkScript,
					},
				},
			},
		}

		result, err := executor.RunTransformHooks(context.Background(), img, inputFile)
		require.NoError(t, err)
		defer result.Cleanup()

		envContent, err := os.ReadFile(outputFile) //nolint:gosec // G304: Test file path
		require.NoError(t, err)
		assert.Contains(t, string(envContent), filepath.Join(cacheDir, "hooks", "env-check"))
	})
}

func TestCopyToTemp(t *testing.T) {
	t.Run("creates copy of file", func(t *testing.T) {
		dir := t.TempDir()
		srcFile := filepath.Join(dir, "source.txt")
		content := "test content for copy"
		err := os.WriteFile(srcFile, []byte(content), 0o600)
		require.NoError(t, err)

		dstPath, err := copyToTemp(srcFile)
		require.NoError(t, err)
		defer func() { _ = os.Remove(dstPath) }()

		// Verify content matches
		dstContent, err := os.ReadFile(dstPath) //nolint:gosec
		require.NoError(t, err)
		assert.Equal(t, content, string(dstContent))

		// Verify it's a different file
		assert.NotEqual(t, srcFile, dstPath)
	})

	t.Run("returns error for nonexistent file", func(t *testing.T) {
		_, err := copyToTemp("/nonexistent/file.txt")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "open source")
	})
}

func TestSanitizeHookName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with-dash", "with-dash"},
		{"with_underscore", "with_underscore"},
		{"with spaces", "with_spaces"},
		{"with/slashes", "with_slashes"},
		{"with:colons", "with_colons"},
		{"MixedCase123", "MixedCase123"},
		{"special!@#$chars", "special____chars"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sanitizeHookName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
