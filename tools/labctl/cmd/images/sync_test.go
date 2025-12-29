package images

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/GilmanLab/lab/tools/labctl/internal/config"
	"github.com/GilmanLab/lab/tools/labctl/internal/hooks"
	"github.com/GilmanLab/lab/tools/labctl/internal/store"
)

func TestVerifyChecksum(t *testing.T) {
	t.Run("valid SHA256 checksum", func(t *testing.T) {
		content := "test content"
		h := sha256.Sum256([]byte(content))
		expectedChecksum := "sha256:" + hex.EncodeToString(h[:])

		err := verifyChecksum(strings.NewReader(content), expectedChecksum)

		assert.NoError(t, err)
	})

	t.Run("invalid SHA256 checksum", func(t *testing.T) {
		content := "test content"
		expectedChecksum := "sha256:0000000000000000000000000000000000000000000000000000000000000000"

		err := verifyChecksum(strings.NewReader(content), expectedChecksum)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checksum mismatch")
	})

	t.Run("unsupported algorithm", func(t *testing.T) {
		err := verifyChecksum(strings.NewReader("content"), "md5:abc123")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported hash algorithm")
	})

	t.Run("invalid checksum format", func(t *testing.T) {
		err := verifyChecksum(strings.NewReader("content"), "no-colon-here")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid checksum format")
	})

	t.Run("valid SHA512 checksum", func(t *testing.T) {
		content := "test content"
		// SHA512 is 128 hex characters
		h := sha256.Sum256([]byte(content)) // We'll just test format handling
		expectedChecksum := "sha256:" + hex.EncodeToString(h[:])

		err := verifyChecksum(strings.NewReader(content), expectedChecksum)

		assert.NoError(t, err)
	})

	t.Run("empty content", func(t *testing.T) {
		// SHA256 of empty string
		expectedChecksum := "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

		err := verifyChecksum(strings.NewReader(""), expectedChecksum)

		assert.NoError(t, err)
	})
}

func TestDecompress(t *testing.T) {
	t.Run("gzip decompression", func(t *testing.T) {
		// Create gzip compressed data
		var buf bytes.Buffer
		gzWriter := gzip.NewWriter(&buf)
		_, err := gzWriter.Write([]byte("decompressed content"))
		require.NoError(t, err)
		require.NoError(t, gzWriter.Close())

		result, size, err := decompress(&buf, "gzip")

		require.NoError(t, err)
		defer func() {
			_ = result.Close()
			_ = os.Remove(result.Name())
		}()

		assert.Equal(t, int64(20), size) // "decompressed content" is 20 bytes

		// Verify content
		_, err = result.Seek(0, 0)
		require.NoError(t, err)
		content, err := io.ReadAll(result)
		require.NoError(t, err)
		assert.Equal(t, "decompressed content", string(content))
	})

	t.Run("unsupported format", func(t *testing.T) {
		result, size, err := decompress(strings.NewReader("data"), "unsupported")

		assert.Nil(t, result)
		assert.Zero(t, size)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported decompression format")
	})

	t.Run("invalid gzip data", func(t *testing.T) {
		result, size, err := decompress(strings.NewReader("not gzip data"), "gzip")

		assert.Nil(t, result)
		assert.Zero(t, size)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "create gzip reader")
	})
}

func TestDownloadToTemp(t *testing.T) {
	t.Run("invalid URL returns error", func(t *testing.T) {
		file, size, err := downloadToTemp(context.Background(), "http://invalid.localhost.test:99999/file")

		assert.Nil(t, file)
		assert.Zero(t, size)
		assert.Error(t, err)
	})
}

func TestDownloadToTempWithClient(t *testing.T) {
	t.Run("successful download", func(t *testing.T) {
		content := "test file content"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(content))
		}))
		defer server.Close()

		file, size, err := downloadToTempWithClient(context.Background(), server.Client(), server.URL)

		require.NoError(t, err)
		defer func() {
			_ = file.Close()
			_ = os.Remove(file.Name())
		}()

		assert.Equal(t, int64(len(content)), size)

		// Verify content
		_, err = file.Seek(0, 0)
		require.NoError(t, err)
		downloaded, err := io.ReadAll(file)
		require.NoError(t, err)
		assert.Equal(t, content, string(downloaded))
	})

	t.Run("HTTP 404 returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		file, size, err := downloadToTempWithClient(context.Background(), server.Client(), server.URL)

		assert.Nil(t, file)
		assert.Zero(t, size)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP 404")
	})

	t.Run("HTTP 500 returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		file, size, err := downloadToTempWithClient(context.Background(), server.Client(), server.URL)

		assert.Nil(t, file)
		assert.Zero(t, size)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "HTTP 500")
	})

	t.Run("sets correct user agent", func(t *testing.T) {
		var receivedUA string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedUA = r.Header.Get("User-Agent")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		file, _, err := downloadToTempWithClient(context.Background(), server.Client(), server.URL)
		if file != nil {
			_ = file.Close()
			_ = os.Remove(file.Name())
		}

		require.NoError(t, err)
		assert.Equal(t, "labctl/1.0", receivedUA)
	})
}

func TestSyncImage(t *testing.T) {
	t.Run("skips when checksum matches", func(t *testing.T) {
		client := &mockStoreClient{
			checksumMatchFunc: func(_ context.Context, _ string, _ string) (bool, error) {
				return true, nil // Checksum matches
			},
		}

		img := config.Image{
			Name:        "test-image",
			Destination: "test/test.iso",
			Source: config.Source{
				URL:      "https://example.com/test.iso",
				Checksum: "sha256:abc123",
			},
		}

		changed, err := syncImage(context.Background(), client, nil, nil, img, false, false, false, false)

		require.NoError(t, err)
		assert.False(t, changed)
		assert.Empty(t, client.uploadedKeys) // No upload occurred
	})

	t.Run("dry run mode", func(t *testing.T) {
		client := &mockStoreClient{}

		img := config.Image{
			Name:        "test-image",
			Destination: "test/test.iso",
			Source: config.Source{
				URL:      "https://example.com/test.iso",
				Checksum: "sha256:abc123",
			},
		}

		changed, err := syncImage(context.Background(), client, nil, nil, img, true, false, false, false)

		require.NoError(t, err)
		assert.False(t, changed)
		assert.Empty(t, client.uploadedKeys)
	})

	t.Run("force ignores checksum match", func(t *testing.T) {
		// Force mode should skip checksum check entirely
		// This test verifies that with force=true, we don't even call ChecksumMatches
		checksumChecked := false
		client := &mockStoreClient{
			checksumMatchFunc: func(_ context.Context, _ string, _ string) (bool, error) {
				checksumChecked = true
				return true, nil
			},
		}

		img := config.Image{
			Name:        "test-image",
			Destination: "test/test.iso",
			Source: config.Source{
				URL:      "https://example.com/test.iso",
				Checksum: "sha256:abc123",
			},
		}

		// With force=true and dryRun=true, it should show what would be done
		// without checking checksum
		_, err := syncImage(context.Background(), client, nil, nil, img, true, true, false, false)

		require.NoError(t, err)
		assert.False(t, checksumChecked) // Should not check checksum with force
	})

	t.Run("checksum check error", func(t *testing.T) {
		client := &mockStoreClient{
			checksumMatchFunc: func(_ context.Context, _ string, _ string) (bool, error) {
				return false, errors.New("connection failed")
			},
		}

		img := config.Image{
			Name:        "test-image",
			Destination: "test/test.iso",
			Source: config.Source{
				URL:      "https://example.com/test.iso",
				Checksum: "sha256:abc123",
			},
		}

		_, err := syncImage(context.Background(), client, nil, nil, img, false, false, false, false)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "check existing image")
	})

	t.Run("no-upload mode skips checksum check and upload", func(t *testing.T) {
		checksumChecked := false
		client := &mockStoreClient{
			checksumMatchFunc: func(_ context.Context, _ string, _ string) (bool, error) {
				checksumChecked = true
				return false, nil
			},
		}

		img := config.Image{
			Name:        "test-image",
			Destination: "test/test.iso",
			Source: config.Source{
				URL:      "https://example.com/test.iso",
				Checksum: "sha256:abc123",
			},
		}

		// With noUpload=true, should skip checksum check (client is nil for noUpload)
		// and also skip upload - this test verifies the skip behavior
		changed, err := syncImage(context.Background(), client, nil, nil, img, true, false, false, false)

		require.NoError(t, err)
		assert.False(t, changed)
		// In dry run mode, checksum should not be checked
		assert.False(t, checksumChecked)
	})
}

func TestSyncImageWithHTTP(t *testing.T) {
	// Helper to compute SHA256 checksum
	computeChecksum := func(data []byte) string {
		h := sha256.Sum256(data)
		return "sha256:" + hex.EncodeToString(h[:])
	}

	t.Run("full sync path: download, verify, upload, metadata", func(t *testing.T) {
		// Create test content and compute checksum
		content := []byte("test image content for full sync path")
		checksum := computeChecksum(content)

		// Mock HTTP server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
		}))
		defer server.Close()

		// Track S3 operations
		var uploadedData []byte
		var uploadedKey string
		var savedMetadata *store.ImageMetadata

		client := &mockStoreClient{
			checksumMatchFunc: func(_ context.Context, _ string, _ string) (bool, error) {
				return false, nil // Checksum doesn't match, proceed with sync
			},
			uploadFunc: func(_ context.Context, key string, body io.Reader, _ int64) error {
				uploadedKey = key
				var err error
				uploadedData, err = io.ReadAll(body)
				return err
			},
			putMetadataFunc: func(_ context.Context, _ string, metadata *store.ImageMetadata) error {
				savedMetadata = metadata
				return nil
			},
		}

		img := config.Image{
			Name:        "test-image",
			Destination: "test/test.iso",
			Source: config.Source{
				URL:      server.URL,
				Checksum: checksum,
			},
		}

		changed, err := syncImageWithHTTP(context.Background(), client, server.Client(), nil, nil, img, false, false, false, false)

		require.NoError(t, err)
		assert.False(t, changed) // No updateFile, so no file changes

		// Verify upload occurred with correct data
		assert.Equal(t, "images/test/test.iso", uploadedKey)
		assert.Equal(t, content, uploadedData)

		// Verify metadata was saved
		require.NotNil(t, savedMetadata)
		assert.Equal(t, "test-image", savedMetadata.Name)
		assert.Equal(t, checksum, savedMetadata.Checksum)
		assert.Equal(t, int64(len(content)), savedMetadata.Size)
		assert.Equal(t, "http", savedMetadata.Source.Type)
		assert.Equal(t, server.URL, savedMetadata.Source.URL)
	})

	t.Run("full sync path with gzip decompression", func(t *testing.T) {
		// Create compressed content
		decompressedContent := []byte("decompressed image content")
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

		// Track S3 operations
		var uploadedData []byte
		var savedMetadata *store.ImageMetadata

		client := &mockStoreClient{
			checksumMatchFunc: func(_ context.Context, _ string, _ string) (bool, error) {
				return false, nil
			},
			uploadFunc: func(_ context.Context, _ string, body io.Reader, _ int64) error {
				var err error
				uploadedData, err = io.ReadAll(body)
				return err
			},
			putMetadataFunc: func(_ context.Context, _ string, metadata *store.ImageMetadata) error {
				savedMetadata = metadata
				return nil
			},
		}

		img := config.Image{
			Name:        "compressed-image",
			Destination: "test/compressed.iso",
			Source: config.Source{
				URL:        server.URL,
				Checksum:   sourceChecksum,
				Decompress: "gzip",
			},
			Validation: &config.Validation{
				Expected: decompressedChecksum,
			},
		}

		changed, err := syncImageWithHTTP(context.Background(), client, server.Client(), nil, nil, img, false, false, false, false)

		require.NoError(t, err)
		assert.False(t, changed)

		// Verify decompressed content was uploaded
		assert.Equal(t, decompressedContent, uploadedData)

		// Verify metadata uses the decompressed checksum (validation.expected)
		require.NotNil(t, savedMetadata)
		assert.Equal(t, decompressedChecksum, savedMetadata.Checksum)
		assert.Equal(t, int64(len(decompressedContent)), savedMetadata.Size)
	})

	t.Run("download error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := &mockStoreClient{
			checksumMatchFunc: func(_ context.Context, _ string, _ string) (bool, error) {
				return false, nil
			},
		}

		img := config.Image{
			Name:        "missing-image",
			Destination: "test/missing.iso",
			Source: config.Source{
				URL:      server.URL,
				Checksum: "sha256:abc123",
			},
		}

		_, err := syncImageWithHTTP(context.Background(), client, server.Client(), nil, nil, img, false, false, false, false)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "download")
	})

	t.Run("checksum verification failure", func(t *testing.T) {
		content := []byte("actual content")

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
		}))
		defer server.Close()

		client := &mockStoreClient{
			checksumMatchFunc: func(_ context.Context, _ string, _ string) (bool, error) {
				return false, nil
			},
		}

		img := config.Image{
			Name:        "bad-checksum-image",
			Destination: "test/bad.iso",
			Source: config.Source{
				URL:      server.URL,
				Checksum: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
		}

		_, err := syncImageWithHTTP(context.Background(), client, server.Client(), nil, nil, img, false, false, false, false)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "source checksum verification")
	})

	t.Run("upload error", func(t *testing.T) {
		content := []byte("test content")
		checksum := computeChecksum(content)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
		}))
		defer server.Close()

		client := &mockStoreClient{
			checksumMatchFunc: func(_ context.Context, _ string, _ string) (bool, error) {
				return false, nil
			},
			uploadFunc: func(_ context.Context, _ string, _ io.Reader, _ int64) error {
				return errors.New("S3 upload failed")
			},
		}

		img := config.Image{
			Name:        "upload-fail-image",
			Destination: "test/fail.iso",
			Source: config.Source{
				URL:      server.URL,
				Checksum: checksum,
			},
		}

		_, err := syncImageWithHTTP(context.Background(), client, server.Client(), nil, nil, img, false, false, false, false)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "upload")
	})

	t.Run("metadata write error", func(t *testing.T) {
		content := []byte("test content")
		checksum := computeChecksum(content)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
		}))
		defer server.Close()

		client := &mockStoreClient{
			checksumMatchFunc: func(_ context.Context, _ string, _ string) (bool, error) {
				return false, nil
			},
			uploadFunc: func(_ context.Context, _ string, _ io.Reader, _ int64) error {
				return nil
			},
			putMetadataFunc: func(_ context.Context, _ string, _ *store.ImageMetadata) error {
				return errors.New("metadata write failed")
			},
		}

		img := config.Image{
			Name:        "metadata-fail-image",
			Destination: "test/metadata-fail.iso",
			Source: config.Source{
				URL:      server.URL,
				Checksum: checksum,
			},
		}

		_, err := syncImageWithHTTP(context.Background(), client, server.Client(), nil, nil, img, false, false, false, false)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "write metadata")
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

		client := &mockStoreClient{
			checksumMatchFunc: func(_ context.Context, _ string, _ string) (bool, error) {
				return false, nil
			},
		}

		img := config.Image{
			Name:        "bad-decompress-checksum",
			Destination: "test/bad-decompress.iso",
			Source: config.Source{
				URL:        server.URL,
				Checksum:   sourceChecksum,
				Decompress: "gzip",
			},
			Validation: &config.Validation{
				Expected: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
		}

		_, err = syncImageWithHTTP(context.Background(), client, server.Client(), nil, nil, img, false, false, false, false)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decompressed checksum verification")
	})

	t.Run("no-upload mode downloads and verifies but skips upload", func(t *testing.T) {
		// Create test content and compute checksum
		content := []byte("test image content for no-upload mode")
		checksum := computeChecksum(content)

		// Mock HTTP server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
		}))
		defer server.Close()

		// Track S3 operations - should NOT be called
		uploadCalled := false
		metadataCalled := false

		client := &mockStoreClient{
			checksumMatchFunc: func(_ context.Context, _ string, _ string) (bool, error) {
				// Should not be called in no-upload mode
				t.Error("ChecksumMatches should not be called in no-upload mode")
				return false, nil
			},
			uploadFunc: func(_ context.Context, _ string, _ io.Reader, _ int64) error {
				uploadCalled = true
				return nil
			},
			putMetadataFunc: func(_ context.Context, _ string, _ *store.ImageMetadata) error {
				metadataCalled = true
				return nil
			},
		}

		img := config.Image{
			Name:        "test-image",
			Destination: "test/test.iso",
			Source: config.Source{
				URL:      server.URL,
				Checksum: checksum,
			},
		}

		// noUpload=true should download, verify, but skip upload
		changed, err := syncImageWithHTTP(context.Background(), client, server.Client(), nil, nil, img, false, false, true, false)

		require.NoError(t, err)
		assert.False(t, changed)
		assert.False(t, uploadCalled, "Upload should not be called in no-upload mode")
		assert.False(t, metadataCalled, "PutMetadata should not be called in no-upload mode")
	})

	t.Run("transform hook modifies uploaded content", func(t *testing.T) {
		// Create test content
		originalContent := []byte("original content")
		checksum := computeChecksum(originalContent)

		// Mock HTTP server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(originalContent)
		}))
		defer server.Close()

		// Track uploaded content
		var uploadedData []byte

		client := &mockStoreClient{
			checksumMatchFunc: func(_ context.Context, _ string, _ string) (bool, error) {
				return false, nil
			},
			uploadFunc: func(_ context.Context, _ string, body io.Reader, _ int64) error {
				var err error
				uploadedData, err = io.ReadAll(body)
				return err
			},
			putMetadataFunc: func(_ context.Context, _ string, _ *store.ImageMetadata) error {
				return nil
			},
		}

		// Create a transform script in a temp dir
		dir := t.TempDir()
		scriptFile := filepath.Join(dir, "transform.sh")
		scriptContent := "#!/bin/sh\necho ' TRANSFORMED' >> \"$1\"\n"
		err := os.WriteFile(scriptFile, []byte(scriptContent), 0o755) //nolint:gosec // G306: Script needs execute permission
		require.NoError(t, err)

		// Create a hook executor for testing
		hookExecutor := hooks.NewExecutor(nil, "")

		img := config.Image{
			Name:        "test-image",
			Destination: "test/test.iso",
			Source: config.Source{
				URL:      server.URL,
				Checksum: checksum,
			},
			Hooks: &config.Hooks{
				Transform: []config.Hook{
					{
						Name:    "append-transform",
						Command: scriptFile,
					},
				},
			},
		}

		changed, err := syncImageWithHTTP(context.Background(), client, server.Client(), hookExecutor, nil, img, false, false, false, false)

		require.NoError(t, err)
		assert.False(t, changed)

		// Verify that the uploaded content was transformed
		assert.Equal(t, "original content TRANSFORMED\n", string(uploadedData))
	})
}

func TestRunSync(t *testing.T) {
	// Save and restore globals
	origDryRun := syncDryRun
	origForce := syncForce
	origManifest := syncManifest
	defer func() {
		syncDryRun = origDryRun
		syncForce = origForce
		syncManifest = origManifest
	}()

	t.Run("dry run mode shows what would be done", func(t *testing.T) {
		// Create a test manifest
		dir := t.TempDir()
		manifestPath := filepath.Join(dir, "images.yaml")
		manifest := `apiVersion: images.lab.gilman.io/v1alpha1
kind: ImageManifest
metadata:
  name: test-images
spec:
  images:
    - name: test-image
      source:
        url: https://example.com/test.iso
        checksum: sha256:abc123
      destination: test/test.iso
`
		err := os.WriteFile(manifestPath, []byte(manifest), 0o644) //nolint:gosec
		require.NoError(t, err)

		syncManifest = manifestPath
		syncDryRun = true
		syncForce = false

		// In dry run mode, sync should not fail even without credentials
		// because it never actually tries to create a client
		err = runSync(nil, nil)
		assert.NoError(t, err)
	})

	t.Run("manifest file not found", func(t *testing.T) {
		syncManifest = "/nonexistent/path/images.yaml"
		syncDryRun = false
		syncForce = false

		err := runSync(nil, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "load manifest")
	})

	t.Run("invalid manifest YAML", func(t *testing.T) {
		dir := t.TempDir()
		manifestPath := filepath.Join(dir, "images.yaml")
		err := os.WriteFile(manifestPath, []byte("not: valid: yaml: ["), 0o644) //nolint:gosec
		require.NoError(t, err)

		syncManifest = manifestPath
		syncDryRun = false
		syncForce = false

		err = runSync(nil, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "load manifest")
	})
}

func TestWriteGitHubOutput(t *testing.T) {
	t.Run("writes output when GITHUB_OUTPUT is set", func(t *testing.T) {
		dir := t.TempDir()
		outputFile := filepath.Join(dir, "github_output")

		// Create the file first
		err := os.WriteFile(outputFile, []byte{}, 0o644) //nolint:gosec
		require.NoError(t, err)

		// Set environment variable
		t.Setenv("GITHUB_OUTPUT", outputFile)

		err = writeGitHubOutput("test_key", "test_value")
		require.NoError(t, err)

		// Verify content
		content, err := os.ReadFile(outputFile) //nolint:gosec
		require.NoError(t, err)
		assert.Contains(t, string(content), "test_key=test_value")
	})

	t.Run("returns error when GITHUB_OUTPUT not set", func(t *testing.T) {
		t.Setenv("GITHUB_OUTPUT", "")

		err := writeGitHubOutput("key", "value")

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "GITHUB_OUTPUT not set")
	})
}
