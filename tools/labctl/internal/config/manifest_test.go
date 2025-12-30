package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseManifest(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "valid manifest with simple image",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: talos-1.9.1
      source:
        url: https://factory.talos.dev/image/metal-amd64.raw.xz
        checksum: sha256:abc123
      destination: talos/talos-1.9.1-amd64.raw
`,
		},
		{
			name: "valid manifest with decompression",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: talos-1.9.1
      source:
        url: https://factory.talos.dev/image/metal-amd64.raw.xz
        checksum: sha256:abc123
        decompress: xz
      destination: talos/talos-1.9.1-amd64.raw
      validation:
        algorithm: sha256
        expected: sha256:def456
`,
		},
		{
			name: "valid manifest with updateFile",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: vyos-iso
      source:
        url: https://github.com/vyos/vyos-rolling-nightly-builds/releases/download/1.5/vyos-1.5.iso
        checksum: sha256:abc123
      destination: vyos/vyos-1.5.iso
      updateFile:
        path: infrastructure/example/vars.hcl
        replacements:
          - pattern: 'vyos_iso_url\s*=\s*"[^"]*"'
            value: 'vyos_iso_url = "{{ .Source.URL }}"'
`,
		},
		{
			name: "invalid apiVersion",
			yaml: `apiVersion: images.lab.gilman.io/v2
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images: []
`,
			wantErr: `unsupported apiVersion "images.lab.gilman.io/v2"`,
		},
		{
			name: "invalid kind",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: SomethingElse
metadata:
  name: lab-images
spec:
  images: []
`,
			wantErr: `unsupported kind "SomethingElse"`,
		},
		{
			name: "missing metadata name",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: ""
spec:
  images: []
`,
			wantErr: "metadata.name is required",
		},
		{
			name: "missing image name",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - source:
        url: https://example.com/image.iso
        checksum: sha256:abc123
      destination: images/image.iso
`,
			wantErr: `image[0] "unnamed-0": name is required`,
		},
		{
			name: "missing source url",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: test-image
      source:
        checksum: sha256:abc123
      destination: images/image.iso
`,
			wantErr: "source.url is required",
		},
		{
			name: "http url rejected",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: test-image
      source:
        url: http://example.com/image.iso
        checksum: sha256:abc123
      destination: images/image.iso
`,
			wantErr: "source.url must use HTTPS",
		},
		{
			name: "missing checksum",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: test-image
      source:
        url: https://example.com/image.iso
      destination: images/image.iso
`,
			wantErr: "source.checksum is required",
		},
		{
			name: "missing destination",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: test-image
      source:
        url: https://example.com/image.iso
        checksum: sha256:abc123
`,
			wantErr: "destination is required",
		},
		{
			name: "invalid decompress format",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: test-image
      source:
        url: https://example.com/image.iso
        checksum: sha256:abc123
        decompress: zip
      destination: images/image.iso
`,
			wantErr: "unsupported decompress format",
		},
		{
			name: "decompress without validation.expected",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: test-image
      source:
        url: https://example.com/image.raw.xz
        checksum: sha256:abc123
        decompress: xz
      destination: images/image.raw
`,
			wantErr: "validation.expected is required when decompress is used",
		},
		{
			name: "invalid validation algorithm",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: test-image
      source:
        url: https://example.com/image.iso
        checksum: sha256:abc123
      destination: images/image.iso
      validation:
        algorithm: md5
        expected: md5:xyz
`,
			wantErr: "unsupported validation algorithm",
		},
		{
			name: "invalid regex pattern",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: test-image
      source:
        url: https://example.com/image.iso
        checksum: sha256:abc123
      destination: images/image.iso
      updateFile:
        path: some/file.txt
        replacements:
          - pattern: '[invalid(regex'
            value: 'replacement'
`,
			wantErr: "pattern is invalid",
		},
		{
			name: "missing updateFile path",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: test-image
      source:
        url: https://example.com/image.iso
        checksum: sha256:abc123
      destination: images/image.iso
      updateFile:
        replacements:
          - pattern: 'foo'
            value: 'bar'
`,
			wantErr: "updateFile.path is required",
		},
		{
			name: "valid manifest with transform hooks",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: talos-iso
      source:
        url: https://factory.talos.dev/image/talos-amd64.iso
        checksum: sha256:abc123
      destination: talos/talos-amd64.iso
      hooks:
        transform:
          - name: embed-config
            command: ./scripts/embed-config.sh
            args: ["--config", "machine.yaml"]
            timeout: 10m
`,
		},
		{
			name: "valid manifest with transform hooks with inputs",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: talos-iso
      source:
        url: https://factory.talos.dev/image/talos-amd64.iso
        checksum: sha256:abc123
      destination: talos/talos-amd64.iso
      hooks:
        transform:
          - name: embed-config
            command: ./scripts/embed-config.sh
            args: ["--config", "machine.yaml"]
            timeout: 10m
            inputs:
              - "infrastructure/compute/talos/talconfig.yaml"
              - "infrastructure/compute/talos/**/*.yaml"
`,
		},
		{
			name: "invalid transform hook missing name",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: test-image
      source:
        url: https://example.com/image.iso
        checksum: sha256:abc123
      destination: images/image.iso
      hooks:
        transform:
          - command: ./script.sh
`,
			wantErr: "hooks.transform[0]: name is required",
		},
		{
			name: "invalid transform hook missing command",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: test-image
      source:
        url: https://example.com/image.iso
        checksum: sha256:abc123
      destination: images/image.iso
      hooks:
        transform:
          - name: my-hook
`,
			wantErr: "hooks.transform[0]: command is required",
		},
		{
			name: "invalid transform hook bad timeout",
			yaml: `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: lab-images
spec:
  images:
    - name: test-image
      source:
        url: https://example.com/image.iso
        checksum: sha256:abc123
      destination: images/image.iso
      hooks:
        transform:
          - name: my-hook
            command: ./script.sh
            timeout: invalid
`,
			wantErr: "hooks.transform[0]: invalid timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest, err := ParseManifest([]byte(tt.yaml))

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, manifest)
		})
	}
}

func TestLoadManifest(t *testing.T) {
	t.Run("file exists", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "images.yaml")

		content := `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: test
spec:
  images:
    - name: test-image
      source:
        url: https://example.com/image.iso
        checksum: sha256:abc123
      destination: images/image.iso
`
		err := os.WriteFile(path, []byte(content), 0o600)
		require.NoError(t, err)

		manifest, err := LoadManifest(path)
		require.NoError(t, err)
		assert.Equal(t, "test", manifest.Metadata.Name)
		assert.Len(t, manifest.Spec.Images, 1)
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := LoadManifest("/nonexistent/path/images.yaml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read manifest file")
	})
}

func TestImage_EffectiveChecksum(t *testing.T) {
	tests := []struct {
		name     string
		image    Image
		expected string
	}{
		{
			name: "uses source checksum when no validation",
			image: Image{
				Source: Source{Checksum: "sha256:source"},
			},
			expected: "sha256:source",
		},
		{
			name: "uses source checksum when validation.expected is empty",
			image: Image{
				Source:     Source{Checksum: "sha256:source"},
				Validation: &Validation{Algorithm: "sha256", Expected: ""},
			},
			expected: "sha256:source",
		},
		{
			name: "uses validation.expected when set",
			image: Image{
				Source:     Source{Checksum: "sha256:source"},
				Validation: &Validation{Algorithm: "sha256", Expected: "sha256:validated"},
			},
			expected: "sha256:validated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.image.EffectiveChecksum())
		})
	}
}

func TestImage_EffectiveChecksumWithInputs(t *testing.T) {
	t.Run("returns base checksum when no hooks", func(t *testing.T) {
		img := Image{
			Source: Source{Checksum: "sha256:abc123"},
		}

		checksum, err := img.EffectiveChecksumWithInputs(t.TempDir())

		require.NoError(t, err)
		assert.Equal(t, "sha256:abc123", checksum)
	})

	t.Run("returns base checksum when hooks have no inputs", func(t *testing.T) {
		img := Image{
			Source: Source{Checksum: "sha256:abc123"},
			Hooks: &Hooks{
				Transform: []Hook{
					{Name: "test-hook", Command: "echo"},
				},
			},
		}

		checksum, err := img.EffectiveChecksumWithInputs(t.TempDir())

		require.NoError(t, err)
		assert.Equal(t, "sha256:abc123", checksum)
	})

	t.Run("incorporates input file hash when inputs are declared", func(t *testing.T) {
		dir := t.TempDir()

		// Create a test input file
		inputFile := filepath.Join(dir, "config.yaml")
		err := os.WriteFile(inputFile, []byte("key: value"), 0o600)
		require.NoError(t, err)

		img := Image{
			Source: Source{Checksum: "sha256:abc123"},
			Hooks: &Hooks{
				Transform: []Hook{
					{
						Name:    "test-hook",
						Command: "echo",
						Inputs:  []string{"config.yaml"},
					},
				},
			},
		}

		checksum, err := img.EffectiveChecksumWithInputs(dir)

		require.NoError(t, err)
		// Checksum should have "+inputs:" suffix with hash
		assert.Contains(t, checksum, "sha256:abc123+inputs:")
		assert.Len(t, checksum, len("sha256:abc123+inputs:")+64) // SHA256 is 64 hex chars
	})

	t.Run("different file contents produce different checksums", func(t *testing.T) {
		dir := t.TempDir()

		// Create first test file
		inputFile := filepath.Join(dir, "config.yaml")
		err := os.WriteFile(inputFile, []byte("key: value1"), 0o600)
		require.NoError(t, err)

		img := Image{
			Source: Source{Checksum: "sha256:abc123"},
			Hooks: &Hooks{
				Transform: []Hook{
					{Name: "test-hook", Command: "echo", Inputs: []string{"config.yaml"}},
				},
			},
		}

		checksum1, err := img.EffectiveChecksumWithInputs(dir)
		require.NoError(t, err)

		// Modify the file
		err = os.WriteFile(inputFile, []byte("key: value2"), 0o600)
		require.NoError(t, err)

		checksum2, err := img.EffectiveChecksumWithInputs(dir)
		require.NoError(t, err)

		assert.NotEqual(t, checksum1, checksum2)
	})

	t.Run("handles glob patterns", func(t *testing.T) {
		dir := t.TempDir()

		// Create test files matching glob
		err := os.WriteFile(filepath.Join(dir, "file1.yaml"), []byte("file1"), 0o600)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(dir, "file2.yaml"), []byte("file2"), 0o600)
		require.NoError(t, err)

		img := Image{
			Source: Source{Checksum: "sha256:abc123"},
			Hooks: &Hooks{
				Transform: []Hook{
					{Name: "test-hook", Command: "echo", Inputs: []string{"*.yaml"}},
				},
			},
		}

		checksum, err := img.EffectiveChecksumWithInputs(dir)

		require.NoError(t, err)
		assert.Contains(t, checksum, "+inputs:")
	})

	t.Run("handles multiple hooks with inputs", func(t *testing.T) {
		dir := t.TempDir()

		err := os.WriteFile(filepath.Join(dir, "config1.yaml"), []byte("config1"), 0o600)
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(dir, "config2.yaml"), []byte("config2"), 0o600)
		require.NoError(t, err)

		img := Image{
			Source: Source{Checksum: "sha256:abc123"},
			Hooks: &Hooks{
				Transform: []Hook{
					{Name: "hook1", Command: "echo", Inputs: []string{"config1.yaml"}},
					{Name: "hook2", Command: "echo", Inputs: []string{"config2.yaml"}},
				},
			},
		}

		checksum, err := img.EffectiveChecksumWithInputs(dir)

		require.NoError(t, err)
		assert.Contains(t, checksum, "+inputs:")
	})

	t.Run("handles validation.expected as base checksum", func(t *testing.T) {
		dir := t.TempDir()

		err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("data"), 0o600)
		require.NoError(t, err)

		img := Image{
			Source:     Source{Checksum: "sha256:source"},
			Validation: &Validation{Expected: "sha256:validated"},
			Hooks: &Hooks{
				Transform: []Hook{
					{Name: "test-hook", Command: "echo", Inputs: []string{"config.yaml"}},
				},
			},
		}

		checksum, err := img.EffectiveChecksumWithInputs(dir)

		require.NoError(t, err)
		// Should use validation.expected as base
		assert.Contains(t, checksum, "sha256:validated+inputs:")
	})

	t.Run("returns empty string for no matching files (glob finds nothing)", func(t *testing.T) {
		dir := t.TempDir()

		img := Image{
			Source: Source{Checksum: "sha256:abc123"},
			Hooks: &Hooks{
				Transform: []Hook{
					{Name: "test-hook", Command: "echo", Inputs: []string{"nonexistent*.yaml"}},
				},
			},
		}

		// Should succeed but produce a checksum based on empty file list
		checksum, err := img.EffectiveChecksumWithInputs(dir)
		require.NoError(t, err)
		// With no files matching, the inputs hash is of empty content
		assert.Contains(t, checksum, "+inputs:")
	})

	t.Run("ignores directories in glob matches", func(t *testing.T) {
		dir := t.TempDir()

		// Create a subdirectory
		subdir := filepath.Join(dir, "subdir")
		err := os.Mkdir(subdir, 0o750)
		require.NoError(t, err)

		// Create a file
		err = os.WriteFile(filepath.Join(dir, "file.yaml"), []byte("file"), 0o600)
		require.NoError(t, err)

		img := Image{
			Source: Source{Checksum: "sha256:abc123"},
			Hooks: &Hooks{
				Transform: []Hook{
					{Name: "test-hook", Command: "echo", Inputs: []string{"*"}},
				},
			},
		}

		// Should succeed and only include the file, not the directory
		checksum, err := img.EffectiveChecksumWithInputs(dir)

		require.NoError(t, err)
		assert.Contains(t, checksum, "+inputs:")
	})
}

func TestImageManifest_FindImageByName(t *testing.T) {
	t.Run("finds existing image", func(t *testing.T) {
		manifest := &ImageManifest{
			Spec: Spec{
				Images: []Image{
					{Name: "image-one", Destination: "path/one"},
					{Name: "image-two", Destination: "path/two"},
				},
			},
		}

		img := manifest.FindImageByName("image-two")

		require.NotNil(t, img)
		assert.Equal(t, "image-two", img.Name)
		assert.Equal(t, "path/two", img.Destination)
	})

	t.Run("returns nil for non-existent image", func(t *testing.T) {
		manifest := &ImageManifest{
			Spec: Spec{
				Images: []Image{
					{Name: "image-one", Destination: "path/one"},
				},
			},
		}

		img := manifest.FindImageByName("non-existent")

		assert.Nil(t, img)
	})

	t.Run("returns nil for empty manifest", func(t *testing.T) {
		manifest := &ImageManifest{}

		img := manifest.FindImageByName("any")

		assert.Nil(t, img)
	})
}
