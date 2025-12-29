package images

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GilmanLab/lab/tools/labctl/internal/config"
)

func TestDownloadImageWithHTTP(t *testing.T) {
	// Helper to compute SHA256 checksum
	computeChecksum := func(data []byte) string {
		h := sha256.Sum256(data)
		return "sha256:" + hex.EncodeToString(h[:])
	}

	t.Run("successful download without decompression", func(t *testing.T) {
		// Create test content and compute checksum
		content := []byte("test image content for download")
		checksum := computeChecksum(content)

		// Mock HTTP server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
		}))
		defer server.Close()

		dir := t.TempDir()
		outputPath := filepath.Join(dir, "output.iso")

		img := config.Image{
			Name:        "test-image",
			Destination: "test/test.iso",
			Source: config.Source{
				URL:      server.URL,
				Checksum: checksum,
			},
		}

		// Capture JSON output
		var stdout bytes.Buffer
		err := downloadImageWithHTTP(context.Background(), server.Client(), img, outputPath, &stdout)

		require.NoError(t, err)

		// Verify output file was created with correct content
		downloaded, err := os.ReadFile(outputPath) //nolint:gosec // G304: Test file path
		require.NoError(t, err)
		assert.Equal(t, content, downloaded)

		// Verify JSON output
		var result DownloadResult
		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, outputPath, result.Path)
		assert.Equal(t, checksum, result.Checksum)
		assert.Equal(t, int64(len(content)), result.Size)
		assert.Equal(t, "test-image", result.Name)
	})

	t.Run("successful download with gzip decompression", func(t *testing.T) {
		// Create compressed content
		decompressedContent := []byte("decompressed image content for download test")
		var compressedBuf bytes.Buffer
		gzWriter := gzip.NewWriter(&compressedBuf)
		_, err := gzWriter.Write(decompressedContent)
		require.NoError(t, err)
		require.NoError(t, gzWriter.Close())
		compressedContent := compressedBuf.Bytes()

		sourceChecksum := computeChecksum(compressedContent)
		decompressedChecksum := computeChecksum(decompressedContent)

		// Mock HTTP server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(compressedContent)
		}))
		defer server.Close()

		dir := t.TempDir()
		outputPath := filepath.Join(dir, "output.iso")

		img := config.Image{
			Name:        "compressed-image",
			Destination: "test/compressed.iso",
			Source: config.Source{
				URL:        server.URL,
				Checksum:   sourceChecksum,
				Decompress: "gzip",
			},
			Validation: &config.Validation{
				Algorithm: "sha256",
				Expected:  decompressedChecksum,
			},
		}

		// Capture JSON output
		var stdout bytes.Buffer
		err = downloadImageWithHTTP(context.Background(), server.Client(), img, outputPath, &stdout)

		require.NoError(t, err)

		// Verify decompressed content was written
		downloaded, err := os.ReadFile(outputPath) //nolint:gosec // G304: Test file path
		require.NoError(t, err)
		assert.Equal(t, decompressedContent, downloaded)

		// Verify JSON output uses decompressed checksum
		var result DownloadResult
		err = json.Unmarshal(stdout.Bytes(), &result)
		require.NoError(t, err)
		assert.Equal(t, decompressedChecksum, result.Checksum)
		assert.Equal(t, int64(len(decompressedContent)), result.Size)
	})

	t.Run("download HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		dir := t.TempDir()
		outputPath := filepath.Join(dir, "output.iso")

		img := config.Image{
			Name:        "missing-image",
			Destination: "test/missing.iso",
			Source: config.Source{
				URL:      server.URL,
				Checksum: "sha256:abc123",
			},
		}

		var stdout bytes.Buffer
		err := downloadImageWithHTTP(context.Background(), server.Client(), img, outputPath, &stdout)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "download")
	})

	t.Run("source checksum verification failure", func(t *testing.T) {
		content := []byte("actual content")

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
		}))
		defer server.Close()

		dir := t.TempDir()
		outputPath := filepath.Join(dir, "output.iso")

		img := config.Image{
			Name:        "bad-checksum",
			Destination: "test/bad.iso",
			Source: config.Source{
				URL:      server.URL,
				Checksum: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
		}

		var stdout bytes.Buffer
		err := downloadImageWithHTTP(context.Background(), server.Client(), img, outputPath, &stdout)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "source checksum verification")
	})

	t.Run("decompressed checksum verification failure", func(t *testing.T) {
		// Create compressed content
		decompressedContent := []byte("decompressed content")
		var compressedBuf bytes.Buffer
		gzWriter := gzip.NewWriter(&compressedBuf)
		_, err := gzWriter.Write(decompressedContent)
		require.NoError(t, err)
		require.NoError(t, gzWriter.Close())
		compressedContent := compressedBuf.Bytes()

		sourceChecksum := computeChecksum(compressedContent)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(compressedContent)
		}))
		defer server.Close()

		dir := t.TempDir()
		outputPath := filepath.Join(dir, "output.iso")

		img := config.Image{
			Name:        "bad-decompress",
			Destination: "test/bad.iso",
			Source: config.Source{
				URL:        server.URL,
				Checksum:   sourceChecksum,
				Decompress: "gzip",
			},
			Validation: &config.Validation{
				Algorithm: "sha256",
				Expected:  "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
		}

		var stdout bytes.Buffer
		err = downloadImageWithHTTP(context.Background(), server.Client(), img, outputPath, &stdout)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decompressed checksum verification")
	})

	t.Run("output path error - invalid directory", func(t *testing.T) {
		content := []byte("test content")
		checksum := computeChecksum(content)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
		}))
		defer server.Close()

		img := config.Image{
			Name:        "test-image",
			Destination: "test/test.iso",
			Source: config.Source{
				URL:      server.URL,
				Checksum: checksum,
			},
		}

		var stdout bytes.Buffer
		err := downloadImageWithHTTP(context.Background(), server.Client(), img, "/nonexistent/directory/output.iso", &stdout)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "create output file")
	})
}

func TestRunDownload(t *testing.T) {
	// Save and restore globals
	origName := downloadName
	origManifest := downloadManifest
	origOutput := downloadOutput
	defer func() {
		downloadName = origName
		downloadManifest = origManifest
		downloadOutput = origOutput
	}()

	t.Run("manifest file not found", func(t *testing.T) {
		downloadName = "test-image"
		downloadManifest = "/nonexistent/path/images.yaml"
		downloadOutput = "/tmp/output.iso"

		err := runDownload(nil, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "load manifest")
	})

	t.Run("image not found in manifest", func(t *testing.T) {
		dir := t.TempDir()
		manifestPath := filepath.Join(dir, "images.yaml")
		manifest := `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: test-images
spec:
  images:
    - name: other-image
      source:
        url: https://example.com/test.iso
        checksum: sha256:abc123
      destination: test/test.iso
`
		err := os.WriteFile(manifestPath, []byte(manifest), 0o644) //nolint:gosec
		require.NoError(t, err)

		downloadName = "non-existent"
		downloadManifest = manifestPath
		downloadOutput = filepath.Join(dir, "output.iso")

		err = runDownload(nil, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found in manifest")
	})
}
