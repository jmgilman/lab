package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewManager(t *testing.T) {
	t.Run("creates directories", func(t *testing.T) {
		dir := t.TempDir()
		baseDir := filepath.Join(dir, "cache")

		m, err := NewManager(baseDir)
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		// Verify directories were created
		for _, subdir := range []string{"downloads", "hooks"} {
			path := filepath.Join(baseDir, subdir)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Errorf("directory %s was not created", subdir)
			}
		}

		if m.BaseDir() != baseDir {
			t.Errorf("BaseDir() = %q, want %q", m.BaseDir(), baseDir)
		}
	})

	t.Run("returns error for empty path", func(t *testing.T) {
		_, err := NewManager("")
		if err == nil {
			t.Error("NewManager(\"\") should return error")
		}
	})
}

func TestManager_GetPut(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	checksum := "sha256:abc123def456"
	content := "test content"

	t.Run("Get returns false for missing file", func(t *testing.T) {
		_, ok := m.Get(checksum)
		if ok {
			t.Error("Get() should return false for missing file")
		}
	})

	t.Run("Put stores and Get retrieves", func(t *testing.T) {
		path, err := m.Put(checksum, strings.NewReader(content))
		if err != nil {
			t.Fatalf("Put() error = %v", err)
		}

		// Verify file was created
		data, err := os.ReadFile(path) //nolint:gosec // G304: Test file path
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", path, err)
		}
		if string(data) != content {
			t.Errorf("file content = %q, want %q", string(data), content)
		}

		// Verify Get returns the path
		gotPath, ok := m.Get(checksum)
		if !ok {
			t.Error("Get() should return true after Put()")
		}
		if gotPath != path {
			t.Errorf("Get() path = %q, want %q", gotPath, path)
		}
	})

	t.Run("Remove deletes cached file", func(t *testing.T) {
		err := m.Remove(checksum)
		if err != nil {
			t.Fatalf("Remove() error = %v", err)
		}

		_, ok := m.Get(checksum)
		if ok {
			t.Error("Get() should return false after Remove()")
		}
	})
}

func TestManager_HookDir(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	t.Run("creates hook directory", func(t *testing.T) {
		hookDir, err := m.HookDir("vyos-integration-test")
		if err != nil {
			t.Fatalf("HookDir() error = %v", err)
		}

		if _, err := os.Stat(hookDir); os.IsNotExist(err) {
			t.Error("HookDir() did not create directory")
		}

		// Verify it's under the hooks subdirectory
		if !strings.Contains(hookDir, "hooks") {
			t.Errorf("HookDir() = %q, expected to contain 'hooks'", hookDir)
		}
	})

	t.Run("sanitizes hook name", func(t *testing.T) {
		hookDir, err := m.HookDir("hook/with:special*chars")
		if err != nil {
			t.Fatalf("HookDir() error = %v", err)
		}

		// Path should not contain special characters
		base := filepath.Base(hookDir)
		for _, char := range []string{"/", ":", "*"} {
			if strings.Contains(base, char) {
				t.Errorf("HookDir base %q contains special char %q", base, char)
			}
		}
	})
}

func TestChecksumKey(t *testing.T) {
	tests := []struct {
		name     string
		checksum string
	}{
		{"with prefix", "sha256:abc123def456"},
		{"without prefix", "abc123def456"},
		{"sha512 prefix", "sha512:abc123def456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := checksumKey(tt.checksum)

			// Key should be exactly 12 hex characters
			if len(key) != 12 {
				t.Errorf("checksumKey() length = %d, want 12", len(key))
			}

			// Key should only contain hex characters
			for _, c := range key {
				isDigit := c >= '0' && c <= '9'
				isHexLetter := c >= 'a' && c <= 'f'
				if !isDigit && !isHexLetter {
					t.Errorf("checksumKey() contains non-hex char: %c", c)
				}
			}
		})
	}
}
