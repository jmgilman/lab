package images

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/GilmanLab/lab/tools/labctl/internal/config"
)

// DownloadResult contains the output of a successful download.
type DownloadResult struct {
	Path     string `json:"path"`
	Checksum string `json:"checksum"`
	Size     int64  `json:"size"`
	Name     string `json:"name"`
}

var downloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Download an image from manifest",
	Long: `Download an image from the manifest to a local file.

The download command looks up an image by name in the manifest,
downloads it from the source URL, verifies the checksum, and
optionally decompresses it. The result is output as JSON for
use in CI/CD workflows.`,
	RunE: runDownload,
}

var (
	downloadName     string
	downloadManifest string
	downloadOutput   string
)

func init() {
	downloadCmd.Flags().StringVar(&downloadName, "name", "", "Image name to download (required)")
	downloadCmd.Flags().StringVar(&downloadManifest, "manifest", "./images/images.yaml", "Path to images.yaml")
	downloadCmd.Flags().StringVar(&downloadOutput, "output", "", "Output file path (required)")

	_ = downloadCmd.MarkFlagRequired("name")
	_ = downloadCmd.MarkFlagRequired("output")
}

func runDownload(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Load manifest
	manifest, err := config.LoadManifest(downloadManifest)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	// Find image by name
	img := manifest.FindImageByName(downloadName)
	if img == nil {
		return fmt.Errorf("image %q not found in manifest", downloadName)
	}

	return downloadImageWithHTTP(ctx, http.DefaultClient, *img, downloadOutput, os.Stdout)
}

// downloadImageWithHTTP downloads an image using the provided HTTP client.
// This function enables dependency injection for testing.
func downloadImageWithHTTP(ctx context.Context, httpClient HTTPClient, img config.Image, outputPath string, out io.Writer) error {
	fmt.Fprintf(os.Stderr, "Downloading %s from %s\n", img.Name, img.Source.URL)

	// Download to temp file
	tempFile, size, err := downloadToTempWithClient(ctx, httpClient, img.Source.URL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempFile.Name())
	}()

	// Verify source checksum
	fmt.Fprintf(os.Stderr, "Verifying source checksum...\n")
	if _, err := tempFile.Seek(0, 0); err != nil {
		return fmt.Errorf("seek temp file: %w", err)
	}
	if err := verifyChecksum(tempFile, img.Source.Checksum); err != nil {
		return fmt.Errorf("source checksum verification: %w", err)
	}

	// Decompress if needed
	var finalFile *os.File
	var finalSize int64
	var finalChecksum string

	if img.Source.Decompress != "" {
		fmt.Fprintf(os.Stderr, "Decompressing (%s)...\n", img.Source.Decompress)
		if _, err := tempFile.Seek(0, 0); err != nil {
			return fmt.Errorf("seek temp file: %w", err)
		}
		decompFile, decompSize, err := decompress(tempFile, img.Source.Decompress)
		if err != nil {
			return fmt.Errorf("decompress: %w", err)
		}
		defer func() {
			_ = decompFile.Close()
			_ = os.Remove(decompFile.Name())
		}()

		// Verify decompressed checksum if validation is specified
		if img.Validation != nil && img.Validation.Expected != "" {
			fmt.Fprintf(os.Stderr, "Verifying decompressed checksum...\n")
			if _, err := decompFile.Seek(0, 0); err != nil {
				return fmt.Errorf("seek decompressed file: %w", err)
			}
			if err := verifyChecksum(decompFile, img.Validation.Expected); err != nil {
				return fmt.Errorf("decompressed checksum verification: %w", err)
			}
		}

		finalFile = decompFile
		finalSize = decompSize
		finalChecksum = img.EffectiveChecksum()
	} else {
		finalFile = tempFile
		finalSize = size
		finalChecksum = img.Source.Checksum
	}

	// Copy to output path
	fmt.Fprintf(os.Stderr, "Writing to %s (%s)\n", outputPath, formatSize(finalSize))
	if _, err := finalFile.Seek(0, 0); err != nil {
		return fmt.Errorf("seek final file: %w", err)
	}

	outFile, err := os.Create(outputPath) //nolint:gosec // G304: Path is provided by user
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}
	defer func() { _ = outFile.Close() }()

	if _, err := io.Copy(outFile, finalFile); err != nil {
		return fmt.Errorf("copy to output: %w", err)
	}

	// Output result as JSON
	result := DownloadResult{
		Path:     outputPath,
		Checksum: finalChecksum,
		Size:     finalSize,
		Name:     img.Name,
	}

	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
