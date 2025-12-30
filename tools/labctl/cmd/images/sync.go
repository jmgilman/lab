package images

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/spf13/cobra"
	"github.com/ulikunitz/xz"

	"github.com/GilmanLab/lab/tools/labctl/internal/cache"
	"github.com/GilmanLab/lab/tools/labctl/internal/config"
	"github.com/GilmanLab/lab/tools/labctl/internal/credentials"
	"github.com/GilmanLab/lab/tools/labctl/internal/hooks"
	"github.com/GilmanLab/lab/tools/labctl/internal/store"
	"github.com/GilmanLab/lab/tools/labctl/internal/updater"
)

// HTTPClient defines the interface for HTTP operations.
// This enables dependency injection for testing.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync images to e2 storage",
	Long: `Download source images, upload to e2, update files, and create PR if needed.

The sync command reads the image manifest, downloads any new or updated images,
uploads them to e2 storage, and optionally updates file references to trigger
downstream builds.`,
	RunE: runSync,
}

var (
	syncManifest           string
	syncCredentials        string
	syncSOPSAgeKeyFile     string
	syncDryRun             bool
	syncForce              bool
	syncSkipHooks          bool
	syncSkipTransformHooks bool
	syncNoUpload           bool
	syncCacheDir           string
)

func init() {
	syncCmd.Flags().StringVar(&syncManifest, "manifest", "./images/images.yaml", "Path to images.yaml")
	syncCmd.Flags().StringVar(&syncCredentials, "credentials", "", "Path to SOPS-encrypted credentials file")
	syncCmd.Flags().StringVar(&syncSOPSAgeKeyFile, "sops-age-key-file", "", "Path to age private key for SOPS decryption")
	syncCmd.Flags().BoolVar(&syncDryRun, "dry-run", false, "Show what would be done without executing")
	syncCmd.Flags().BoolVar(&syncForce, "force", false, "Force re-upload even if checksums match")
	syncCmd.Flags().BoolVar(&syncSkipHooks, "skip-hooks", false, "Skip pre-upload hooks")
	syncCmd.Flags().BoolVar(&syncSkipTransformHooks, "skip-transform-hooks", false, "Skip transform hooks (for CI without specialized tools)")
	syncCmd.Flags().BoolVar(&syncNoUpload, "no-upload", false, "Download and run hooks but skip upload (for testing)")
	syncCmd.Flags().StringVar(&syncCacheDir, "cache-dir", "", "Local cache directory for downloads and hooks")
}

func runSync(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Load manifest
	manifest, err := config.LoadManifest(syncManifest)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	fmt.Printf("Syncing images from manifest: %s\n", syncManifest)
	fmt.Printf("Found %d image(s)\n\n", len(manifest.Spec.Images))

	// Set up local cache if configured
	var cacheManager *cache.Manager
	if syncCacheDir != "" {
		var err error
		cacheManager, err = cache.NewManager(syncCacheDir)
		if err != nil {
			return fmt.Errorf("create cache manager: %w", err)
		}
		fmt.Printf("Using cache directory: %s\n\n", syncCacheDir)
	}

	// Skip credentials and S3 client setup in dry-run or no-upload mode
	var client *store.S3Client
	var hookExecutor *hooks.Executor
	if !syncDryRun && !syncNoUpload {
		// Resolve credentials
		creds, err := credentials.Resolve(credentials.ResolveOptions{
			SOPSFile:   syncCredentials,
			AgeKeyFile: syncSOPSAgeKeyFile,
		})
		if err != nil {
			return fmt.Errorf("resolve credentials: %w", err)
		}

		// Create S3 client
		client, err = store.NewS3Client(creds, store.WithContext(ctx))
		if err != nil {
			return fmt.Errorf("create S3 client: %w", err)
		}

		// Create hook executor with caching (unless skipped)
		if !syncSkipHooks {
			hookExecutor = hooks.NewExecutor(client, syncCacheDir)
		}
	} else if syncNoUpload && !syncSkipHooks {
		// In no-upload mode, create hook executor without S3 caching
		hookExecutor = hooks.NewExecutor(nil, syncCacheDir)
	}

	// Track if any files were changed (for GitHub Actions output)
	filesChanged := false

	// Determine base directory for resolving hook input paths.
	// Use the manifest's parent directory as the base.
	manifestDir := filepath.Dir(syncManifest)
	baseDir, err := filepath.Abs(manifestDir)
	if err != nil {
		return fmt.Errorf("resolve manifest directory: %w", err)
	}
	// Go up one level from images/ to repo root for input path resolution
	baseDir = filepath.Dir(baseDir)

	// Process each image
	for _, img := range manifest.Spec.Images {
		changed, err := syncImageWithHTTP(ctx, client, http.DefaultClient, hookExecutor, cacheManager, img, baseDir, syncDryRun, syncForce, syncNoUpload, syncSkipTransformHooks)
		if err != nil {
			return fmt.Errorf("sync image %q: %w", img.Name, err)
		}
		if changed {
			filesChanged = true
		}
	}

	// Write GitHub Actions output
	if err := writeGitHubOutput("files_changed", fmt.Sprintf("%t", filesChanged)); err != nil {
		// Log but don't fail - not running in GitHub Actions
		fmt.Printf("Note: Could not write GitHub Actions output: %v\n", err)
	}

	fmt.Println("\nSync complete")
	if filesChanged {
		fmt.Println("Files were changed - PR may be needed")
	}

	return nil
}

// syncImage syncs a single image using the default HTTP client.
// This is a convenience wrapper for syncImageWithHTTP.
func syncImage(ctx context.Context, client store.Client, hookExecutor *hooks.Executor, cacheManager *cache.Manager, img config.Image, baseDir string, dryRun, force, noUpload, skipTransformHooks bool) (bool, error) {
	return syncImageWithHTTP(ctx, client, http.DefaultClient, hookExecutor, cacheManager, img, baseDir, dryRun, force, noUpload, skipTransformHooks)
}

// syncImageWithHTTP syncs an image using the provided HTTP and store clients.
// This function enables dependency injection for testing.
// The baseDir parameter specifies the directory for resolving hook input paths.
func syncImageWithHTTP(ctx context.Context, client store.Client, httpClient HTTPClient, hookExecutor *hooks.Executor, cacheManager *cache.Manager, img config.Image, baseDir string, dryRun, force, noUpload, skipTransformHooks bool) (bool, error) {
	fmt.Printf("Processing: %s\n", img.Name)

	// Compute effective checksum including hook input files
	effectiveChecksum, err := img.EffectiveChecksumWithInputs(baseDir)
	if err != nil {
		return false, fmt.Errorf("compute effective checksum: %w", err)
	}

	// Check if image already exists with matching checksum (skip in no-upload mode)
	if !dryRun && !force && !noUpload {
		matches, err := client.ChecksumMatches(ctx, img.Destination, effectiveChecksum)
		if err != nil {
			return false, fmt.Errorf("check existing image: %w", err)
		}
		if matches {
			fmt.Printf("  Skipping: checksum matches existing image\n")
			return false, nil
		}
	}

	if dryRun {
		fmt.Printf("  Would download: %s\n", img.Source.URL)
		fmt.Printf("  Would upload to: %s\n", store.ImageKey(img.Destination))
		if img.UpdateFile != nil {
			fmt.Printf("  Would update file: %s\n", img.UpdateFile.Path)
		}
		return false, nil
	}

	// Download source image (with cache check)
	var tempFile *os.File
	var size int64
	var fromCache bool

	if cacheManager != nil {
		if cachePath, ok := cacheManager.Get(img.Source.Checksum); ok {
			// Found in cache - verify checksum before using
			fmt.Printf("  Using cached: %s\n", cachePath)
			f, err := os.Open(cachePath) //nolint:gosec // G304: Path from trusted cache manager
			if err != nil {
				// Cache file not accessible, fall through to download
				fmt.Printf("  Cache error, will download: %v\n", err)
			} else {
				// Verify cached file checksum
				if err := verifyChecksum(f, img.Source.Checksum); err != nil {
					fmt.Printf("  Cache checksum mismatch, will download: %v\n", err)
					_ = f.Close()
					_ = cacheManager.Remove(img.Source.Checksum)
				} else {
					if _, err := f.Seek(0, 0); err != nil {
						_ = f.Close()
						return false, fmt.Errorf("seek cached file: %w", err)
					}
					stat, _ := f.Stat()
					tempFile = f
					size = stat.Size()
					fromCache = true
				}
			}
		}
	}

	if tempFile == nil {
		// Not in cache or cache disabled - download
		fmt.Printf("  Downloading from: %s\n", img.Source.URL)
		var err error
		tempFile, size, err = downloadToTempWithClient(ctx, httpClient, img.Source.URL)
		if err != nil {
			return false, fmt.Errorf("download: %w", err)
		}

		// Verify source checksum
		fmt.Printf("  Verifying source checksum...\n")
		if _, err := tempFile.Seek(0, 0); err != nil {
			_ = tempFile.Close()
			_ = os.Remove(tempFile.Name())
			return false, fmt.Errorf("seek temp file: %w", err)
		}
		if err := verifyChecksum(tempFile, img.Source.Checksum); err != nil {
			_ = tempFile.Close()
			_ = os.Remove(tempFile.Name())
			return false, fmt.Errorf("source checksum verification: %w", err)
		}

		// Store in cache for future use
		if cacheManager != nil {
			if _, err := tempFile.Seek(0, 0); err != nil {
				_ = tempFile.Close()
				_ = os.Remove(tempFile.Name())
				return false, fmt.Errorf("seek temp file for cache: %w", err)
			}
			cachePath, err := cacheManager.Put(img.Source.Checksum, tempFile)
			if err != nil {
				// Log but don't fail - caching is optional
				fmt.Printf("  Warning: failed to cache: %v\n", err)
			} else {
				fmt.Printf("  Cached to: %s\n", cachePath)
			}
		}
	}

	// Set up cleanup - only remove temp files, not cached files
	defer func() {
		_ = tempFile.Close()
		if !fromCache {
			_ = os.Remove(tempFile.Name())
		}
	}()

	// Reset file position after checksum verification
	if _, err := tempFile.Seek(0, 0); err != nil {
		return false, fmt.Errorf("seek file: %w", err)
	}

	// Decompress if needed
	var uploadFile *os.File
	var uploadSize int64
	if img.Source.Decompress != "" {
		fmt.Printf("  Decompressing (%s)...\n", img.Source.Decompress)
		if _, err := tempFile.Seek(0, 0); err != nil {
			return false, fmt.Errorf("seek temp file: %w", err)
		}
		decompFile, decompSize, err := decompress(tempFile, img.Source.Decompress)
		if err != nil {
			return false, fmt.Errorf("decompress: %w", err)
		}
		defer func() {
			_ = decompFile.Close()
			_ = os.Remove(decompFile.Name())
		}()

		// Verify post-decompression checksum if validation is specified
		if img.Validation != nil && img.Validation.Expected != "" {
			fmt.Printf("  Verifying decompressed checksum...\n")
			if _, err := decompFile.Seek(0, 0); err != nil {
				return false, fmt.Errorf("seek decompressed file: %w", err)
			}
			if err := verifyChecksum(decompFile, img.Validation.Expected); err != nil {
				return false, fmt.Errorf("decompressed checksum verification: %w", err)
			}
		}

		uploadFile = decompFile
		uploadSize = decompSize
	} else {
		uploadFile = tempFile
		uploadSize = size
	}

	// Run transform hooks (before pre-upload hooks)
	if img.Hooks != nil && len(img.Hooks.Transform) > 0 {
		if skipTransformHooks {
			fmt.Printf("  Skipping transform hooks (--skip-transform-hooks)\n")
		} else if hookExecutor != nil {
			fmt.Printf("  Running transform hooks...\n")
			result, err := hookExecutor.RunTransformHooks(ctx, img, uploadFile.Name())
			if err != nil {
				return false, fmt.Errorf("transform hooks: %w", err)
			}
			defer result.Cleanup()

			if result.OutputPath != "" {
				// Use transformed file for upload
				transformedFile, err := os.Open(result.OutputPath) //nolint:gosec // G304: Path from trusted hook result
				if err != nil {
					return false, fmt.Errorf("open transformed file: %w", err)
				}
				defer func() { _ = transformedFile.Close() }()

				stat, err := transformedFile.Stat()
				if err != nil {
					return false, fmt.Errorf("stat transformed file: %w", err)
				}

				uploadFile = transformedFile
				uploadSize = stat.Size()
			}
		}
	}

	// Run pre-upload hooks
	if hookExecutor != nil && img.Hooks != nil && len(img.Hooks.PreUpload) > 0 {
		fmt.Printf("  Running pre-upload hooks...\n")
		if err := hookExecutor.RunPreUploadHooks(ctx, img, uploadFile.Name(), effectiveChecksum); err != nil {
			return false, fmt.Errorf("pre-upload hooks: %w", err)
		}
	}

	// Skip upload in no-upload mode (used for PR testing)
	if noUpload {
		fmt.Printf("  Skipping upload (--no-upload mode)\n")
		fmt.Printf("  Done\n")
		return false, nil
	}

	// Upload to e2
	if _, err := uploadFile.Seek(0, 0); err != nil {
		return false, fmt.Errorf("seek upload file: %w", err)
	}
	imageKey := store.ImageKey(img.Destination)
	fmt.Printf("  Uploading to: %s (%s)\n", imageKey, formatSize(uploadSize))
	if err := client.Upload(ctx, imageKey, uploadFile, uploadSize); err != nil {
		return false, fmt.Errorf("upload: %w", err)
	}

	// Write metadata
	metadata := &store.ImageMetadata{
		Name:       img.Name,
		Checksum:   effectiveChecksum,
		Size:       uploadSize,
		UploadedAt: time.Now().UTC(),
		Source: store.SourceMetadata{
			Type: "http",
			URL:  img.Source.URL,
		},
	}
	if err := client.PutMetadata(ctx, img.Destination, metadata); err != nil {
		return false, fmt.Errorf("write metadata: %w", err)
	}

	// Apply file updates if specified
	filesChanged := false
	if img.UpdateFile != nil {
		fmt.Printf("  Updating file: %s\n", img.UpdateFile.Path)

		replacements := make([]updater.Replacement, len(img.UpdateFile.Replacements))
		for i, r := range img.UpdateFile.Replacements {
			replacements[i] = updater.Replacement{
				Pattern: r.Pattern,
				Value:   r.Value,
			}
		}

		data := updater.TemplateData{
			Source: updater.SourceData{
				URL:      img.Source.URL,
				Checksum: img.Source.Checksum,
			},
		}

		fileUpdater, err := updater.New(replacements, data)
		if err != nil {
			return false, fmt.Errorf("create file updater: %w", err)
		}

		modified, err := fileUpdater.UpdateFile(img.UpdateFile.Path)
		if err != nil {
			return false, fmt.Errorf("update file: %w", err)
		}

		if modified {
			fmt.Printf("  File updated: %s\n", img.UpdateFile.Path)
			filesChanged = true
		} else {
			fmt.Printf("  File unchanged: %s\n", img.UpdateFile.Path)
		}
	}

	fmt.Printf("  Done\n")
	return filesChanged, nil
}

// downloadToTemp downloads a URL to a temp file using the default HTTP client.
func downloadToTemp(ctx context.Context, url string) (*os.File, int64, error) {
	return downloadToTempWithClient(ctx, http.DefaultClient, url)
}

// downloadToTempWithClient downloads a URL to a temp file using the provided HTTP client.
// This function enables dependency injection for testing.
func downloadToTempWithClient(ctx context.Context, client HTTPClient, url string) (*os.File, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "labctl/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("HTTP request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	tempFile, err := os.CreateTemp("", "labctl-download-*")
	if err != nil {
		return nil, 0, fmt.Errorf("create temp file: %w", err)
	}

	size, err := io.Copy(tempFile, resp.Body)
	if err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempFile.Name())
		return nil, 0, fmt.Errorf("write to temp file: %w", err)
	}

	return tempFile, size, nil
}

func verifyChecksum(r io.Reader, expected string) error {
	// Parse expected checksum format: "sha256:abc123..." or "sha512:..."
	parts := strings.SplitN(expected, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid checksum format: %s", expected)
	}

	algorithm := parts[0]
	expectedHash := parts[1]

	var h hash.Hash
	switch algorithm {
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	default:
		return fmt.Errorf("unsupported hash algorithm: %s", algorithm)
	}

	if _, err := io.Copy(h, r); err != nil {
		return fmt.Errorf("compute hash: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actual)
	}

	return nil
}

// maxDecompressedSize limits decompressed file size to 50GB to prevent decompression bombs.
const maxDecompressedSize = 50 * 1024 * 1024 * 1024

func decompress(r io.Reader, format string) (*os.File, int64, error) {
	var reader io.Reader
	var cleanup func()

	switch format {
	case "xz":
		xzReader, err := xz.NewReader(r)
		if err != nil {
			return nil, 0, fmt.Errorf("create xz reader: %w", err)
		}
		reader = xzReader
	case "gzip":
		gzReader, err := gzip.NewReader(r)
		if err != nil {
			return nil, 0, fmt.Errorf("create gzip reader: %w", err)
		}
		reader = gzReader
		cleanup = func() { _ = gzReader.Close() }
	case "zstd":
		zstdReader, err := zstd.NewReader(r)
		if err != nil {
			return nil, 0, fmt.Errorf("create zstd reader: %w", err)
		}
		reader = zstdReader
		cleanup = func() { zstdReader.Close() }
	default:
		return nil, 0, fmt.Errorf("unsupported decompression format: %s", format)
	}

	// Wrap with a limit reader to prevent decompression bombs
	limitedReader := io.LimitReader(reader, maxDecompressedSize)

	tempFile, err := os.CreateTemp("", "labctl-decompress-*")
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, 0, fmt.Errorf("create temp file: %w", err)
	}

	size, err := io.Copy(tempFile, limitedReader)
	if cleanup != nil {
		cleanup()
	}
	if err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempFile.Name())
		return nil, 0, fmt.Errorf("decompress to temp file: %w", err)
	}

	return tempFile, size, nil
}

func writeGitHubOutput(name, value string) error {
	outputFile := os.Getenv("GITHUB_OUTPUT")
	if outputFile == "" {
		return fmt.Errorf("GITHUB_OUTPUT not set")
	}

	f, err := os.OpenFile(outputFile, os.O_APPEND|os.O_WRONLY, 0o644) //nolint:gosec // G304: Path from env
	if err != nil {
		return fmt.Errorf("open GITHUB_OUTPUT: %w", err)
	}
	defer func() { _ = f.Close() }()

	_, err = fmt.Fprintf(f, "%s=%s\n", name, value)
	return err
}
