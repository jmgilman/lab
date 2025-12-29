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
