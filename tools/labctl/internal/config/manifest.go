// Package config provides configuration parsing for the image pipeline.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SupportedAPIVersion is the supported API version for the image manifest.
const SupportedAPIVersion = "images.lab.gilman.io/v1alpha1"

// ImageManifest represents the top-level image manifest configuration.
type ImageManifest struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Spec     `yaml:"spec"`
}

// Metadata contains manifest metadata.
type Metadata struct {
	Name string `yaml:"name"`
}

// Spec contains the list of images to manage.
type Spec struct {
	Images []Image `yaml:"images"`
}

// Image represents a single image configuration.
type Image struct {
	Name        string      `yaml:"name"`
	Source      Source      `yaml:"source"`
	Destination string      `yaml:"destination"`
	Validation  *Validation `yaml:"validation,omitempty"`
	UpdateFile  *UpdateFile `yaml:"updateFile,omitempty"`
	Hooks       *Hooks      `yaml:"hooks,omitempty"`
}

// Hooks defines lifecycle hooks for an image.
type Hooks struct {
	// PreUpload runs after download/verification, before upload.
	// Hook must exit 0 for upload to proceed.
	PreUpload []Hook `yaml:"preUpload,omitempty"`
	// Transform runs after download/verification, before upload.
	// The hook receives a copy of the file and can modify it in-place.
	// The modified file becomes the upload source.
	// Transform hooks run before preUpload hooks.
	Transform []Hook `yaml:"transform,omitempty"`
}

// Hook defines a hook to run during image processing.
type Hook struct {
	// Name is a human-readable identifier for the hook.
	Name string `yaml:"name"`
	// Command is the executable to run (path or command name).
	Command string `yaml:"command"`
	// Args are additional arguments to pass to the command.
	// The image path is always passed as the first argument.
	Args []string `yaml:"args,omitempty"`
	// Timeout is the maximum duration for the hook to run.
	// Defaults to 30 minutes if not specified.
	Timeout string `yaml:"timeout,omitempty"`
	// WorkDir is the working directory for the command.
	// If not specified, uses the current working directory.
	WorkDir string `yaml:"workDir,omitempty"`
	// Inputs declares files/globs that affect the hook's output.
	// When specified, changes to these files will trigger a re-sync
	// even if the source checksum matches. Paths are relative to the
	// repository root. Supports glob patterns (e.g., "config/*.yaml").
	Inputs []string `yaml:"inputs,omitempty"`
}

// Source defines where to download the image from.
type Source struct {
	URL        string `yaml:"url"`
	Checksum   string `yaml:"checksum"`
	Decompress string `yaml:"decompress,omitempty"` // xz, gzip, zstd
}

// Validation defines post-processing validation rules.
type Validation struct {
	Algorithm string `yaml:"algorithm"` // sha256, sha512
	Expected  string `yaml:"expected"`
}

// UpdateFile defines file updates to trigger downstream builds.
type UpdateFile struct {
	Path         string        `yaml:"path"`
	Replacements []Replacement `yaml:"replacements"`
}

// Replacement defines a regex-based replacement in a file.
type Replacement struct {
	Pattern string `yaml:"pattern"` // Regex pattern
	Value   string `yaml:"value"`   // Template: {{ .Source.URL }}, {{ .Source.Checksum }}
}

// EffectiveChecksum returns the base checksum to use for idempotency checks.
// If validation.expected is set, use that; otherwise use source.checksum.
// Note: This does not include hook input files. Use EffectiveChecksumWithInputs
// for a checksum that incorporates transform hook input file changes.
func (i *Image) EffectiveChecksum() string {
	if i.Validation != nil && i.Validation.Expected != "" {
		return i.Validation.Expected
	}
	return i.Source.Checksum
}

// EffectiveChecksumWithInputs returns a checksum that incorporates both the
// base checksum and the hash of any input files declared by transform hooks.
// This ensures that changes to transform hook inputs trigger a re-sync.
// If no inputs are declared, returns the base EffectiveChecksum().
// The baseDir parameter specifies the directory relative to which input
// paths are resolved (typically the repository root or manifest directory).
func (i *Image) EffectiveChecksumWithInputs(baseDir string) (string, error) {
	baseChecksum := i.EffectiveChecksum()

	// Collect all inputs from transform hooks
	var allInputs []string
	if i.Hooks != nil {
		for _, h := range i.Hooks.Transform {
			allInputs = append(allInputs, h.Inputs...)
		}
	}

	// If no inputs, return base checksum
	if len(allInputs) == 0 {
		return baseChecksum, nil
	}

	// Compute hash of all input files
	inputsHash, err := hashInputFiles(baseDir, allInputs)
	if err != nil {
		return "", fmt.Errorf("hash input files: %w", err)
	}

	// Combine base checksum with inputs hash
	// Format: "base_checksum+inputs:hash"
	return baseChecksum + "+inputs:" + inputsHash, nil
}

// hashInputFiles computes a combined SHA256 hash of all files matching the
// given glob patterns. Files are processed in sorted order for determinism.
func hashInputFiles(baseDir string, patterns []string) (string, error) {
	// Expand all globs and collect unique file paths
	fileSet := make(map[string]struct{})
	for _, pattern := range patterns {
		fullPattern := filepath.Join(baseDir, pattern)
		matches, err := filepath.Glob(fullPattern)
		if err != nil {
			return "", fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
		}
		for _, match := range matches {
			// Only include regular files, not directories
			info, err := os.Stat(match)
			if err != nil {
				return "", fmt.Errorf("stat %q: %w", match, err)
			}
			if info.Mode().IsRegular() {
				fileSet[match] = struct{}{}
			}
		}
	}

	// Sort file paths for deterministic hashing
	var files []string
	for f := range fileSet {
		files = append(files, f)
	}
	sort.Strings(files)

	// Compute combined hash
	h := sha256.New()
	for _, file := range files {
		// Include relative path in hash (so renames are detected)
		relPath, err := filepath.Rel(baseDir, file)
		if err != nil {
			relPath = file
		}
		h.Write([]byte(relPath))
		h.Write([]byte{0}) // Separator

		// Hash file contents
		f, err := os.Open(file) //nolint:gosec // G304: Paths come from trusted manifest
		if err != nil {
			return "", fmt.Errorf("open %q: %w", file, err)
		}
		if _, err := io.Copy(h, f); err != nil {
			_ = f.Close()
			return "", fmt.Errorf("read %q: %w", file, err)
		}
		_ = f.Close()
		h.Write([]byte{0}) // Separator between files
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// FindImageByName returns the image with the given name, or nil if not found.
func (m *ImageManifest) FindImageByName(name string) *Image {
	for i := range m.Spec.Images {
		if m.Spec.Images[i].Name == name {
			return &m.Spec.Images[i]
		}
	}
	return nil
}

// LoadManifest reads and parses an image manifest from a file.
func LoadManifest(path string) (*ImageManifest, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: Path is provided by user
	if err != nil {
		return nil, fmt.Errorf("read manifest file: %w", err)
	}

	return ParseManifest(data)
}

// ParseManifest parses an image manifest from YAML data.
func ParseManifest(data []byte) (*ImageManifest, error) {
	var manifest ImageManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest YAML: %w", err)
	}

	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("validate manifest: %w", err)
	}

	return &manifest, nil
}

// ParseManifestRaw parses an image manifest from YAML data without validation.
// Use this when you want to collect all validation errors separately.
func ParseManifestRaw(data []byte) (*ImageManifest, error) {
	var manifest ImageManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest YAML: %w", err)
	}
	return &manifest, nil
}

// LoadManifestRaw reads and parses an image manifest without validation.
// Use this when you want to collect all validation errors separately.
func LoadManifestRaw(path string) (*ImageManifest, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: Path is provided by user
	if err != nil {
		return nil, fmt.Errorf("read manifest file: %w", err)
	}
	return ParseManifestRaw(data)
}

// Validate checks that the manifest is well-formed.
func (m *ImageManifest) Validate() error {
	errs := m.ValidateAll()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// ValidateAll checks the manifest and returns all validation errors.
func (m *ImageManifest) ValidateAll() []error {
	var errs []error

	if m.APIVersion != SupportedAPIVersion {
		errs = append(errs, fmt.Errorf("unsupported apiVersion %q, expected %q", m.APIVersion, SupportedAPIVersion))
	}

	if m.Kind != "ImageManifest" {
		errs = append(errs, fmt.Errorf("unsupported kind %q, expected %q", m.Kind, "ImageManifest"))
	}

	if m.Metadata.Name == "" {
		errs = append(errs, fmt.Errorf("metadata.name is required"))
	}

	for i, img := range m.Spec.Images {
		imgName := img.Name
		if imgName == "" {
			imgName = fmt.Sprintf("unnamed-%d", i)
		}
		for _, err := range img.ValidateAll() {
			errs = append(errs, fmt.Errorf("image[%d] %q: %w", i, imgName, err))
		}
	}

	return errs
}

// Validate checks that the image configuration is valid.
func (i *Image) Validate() error {
	errs := i.ValidateAll()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// ValidateAll checks the image configuration and returns all validation errors.
func (i *Image) ValidateAll() []error {
	var errs []error

	if i.Name == "" {
		errs = append(errs, fmt.Errorf("name is required"))
	}

	if i.Source.URL == "" {
		errs = append(errs, fmt.Errorf("source.url is required"))
	} else if !strings.HasPrefix(i.Source.URL, "https://") {
		errs = append(errs, fmt.Errorf("source.url must use HTTPS"))
	}

	if i.Source.Checksum == "" {
		errs = append(errs, fmt.Errorf("source.checksum is required"))
	}

	if i.Destination == "" {
		errs = append(errs, fmt.Errorf("destination is required"))
	}

	// Validate decompress option
	if i.Source.Decompress != "" {
		switch i.Source.Decompress {
		case "xz", "gzip", "zstd":
			// valid
		default:
			errs = append(errs, fmt.Errorf("unsupported decompress format %q, must be xz, gzip, or zstd", i.Source.Decompress))
		}

		// validation.expected is required when decompress is used
		if i.Validation == nil || i.Validation.Expected == "" {
			errs = append(errs, fmt.Errorf("validation.expected is required when decompress is used"))
		}
	}

	// Validate algorithm if validation is specified
	if i.Validation != nil {
		switch i.Validation.Algorithm {
		case "sha256", "sha512":
			// valid
		default:
			errs = append(errs, fmt.Errorf("unsupported validation algorithm %q, must be sha256 or sha512", i.Validation.Algorithm))
		}
	}

	// Validate updateFile regex patterns compile
	if i.UpdateFile != nil {
		if i.UpdateFile.Path == "" {
			errs = append(errs, fmt.Errorf("updateFile.path is required"))
		}

		for j, r := range i.UpdateFile.Replacements {
			if r.Pattern == "" {
				errs = append(errs, fmt.Errorf("updateFile.replacements[%d].pattern is required", j))
			} else if _, err := regexp.Compile(r.Pattern); err != nil {
				errs = append(errs, fmt.Errorf("updateFile.replacements[%d].pattern is invalid: %w", j, err))
			}

			if r.Value == "" {
				errs = append(errs, fmt.Errorf("updateFile.replacements[%d].value is required", j))
			}
		}
	}

	// Validate hooks
	if i.Hooks != nil {
		for j, h := range i.Hooks.PreUpload {
			for _, err := range h.ValidateAll() {
				errs = append(errs, fmt.Errorf("hooks.preUpload[%d]: %w", j, err))
			}
		}
		for j, h := range i.Hooks.Transform {
			for _, err := range h.ValidateAll() {
				errs = append(errs, fmt.Errorf("hooks.transform[%d]: %w", j, err))
			}
		}
	}

	return errs
}

// ValidateAll checks the hook configuration and returns all validation errors.
func (h *Hook) ValidateAll() []error {
	var errs []error

	if h.Name == "" {
		errs = append(errs, fmt.Errorf("name is required"))
	}

	if h.Command == "" {
		errs = append(errs, fmt.Errorf("command is required"))
	}

	if h.Timeout != "" {
		if _, err := time.ParseDuration(h.Timeout); err != nil {
			errs = append(errs, fmt.Errorf("invalid timeout %q: %w", h.Timeout, err))
		}
	}

	return errs
}
