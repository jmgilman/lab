// Package store provides storage operations for the image pipeline.
package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	labcreds "github.com/GilmanLab/lab/tools/labctl/internal/credentials"
)

// Client defines the storage operations used by commands.
// This interface enables dependency injection for testing.
type Client interface {
	Upload(ctx context.Context, key string, body io.Reader, size int64) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Exists(ctx context.Context, key string) (bool, error)
	List(ctx context.Context, prefix string) ([]string, error)
	Delete(ctx context.Context, key string) error
	GetMetadata(ctx context.Context, imagePath string) (*ImageMetadata, error)
	PutMetadata(ctx context.Context, imagePath string, metadata *ImageMetadata) error
	ChecksumMatches(ctx context.Context, imagePath, expectedChecksum string) (bool, error)
	GetHookResult(ctx context.Context, imagePath, hookName string) (*HookResult, error)
	PutHookResult(ctx context.Context, imagePath, hookName string, result *HookResult) error
}

// ImageMetadata represents metadata stored alongside each image.
type ImageMetadata struct {
	Name       string         `json:"name"`
	Checksum   string         `json:"checksum"`
	Size       int64          `json:"size"`
	UploadedAt time.Time      `json:"uploadedAt"`
	Source     SourceMetadata `json:"source"`
}

// SourceMetadata describes the origin of an image.
type SourceMetadata struct {
	// Type is "http" for downloaded images or "local" for uploaded files.
	Type string `json:"type,omitempty"`
	// URL is set for HTTP sources.
	URL string `json:"url,omitempty"`
	// Path is set for local file uploads.
	Path string `json:"path,omitempty"`
}

// s3API defines the S3 operations used by S3Client.
// This interface enables mocking for unit tests.
type s3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

// S3Client wraps the AWS S3 client for image storage operations.
type S3Client struct {
	api    s3API
	bucket string
}

// S3Option configures the S3 client.
type S3Option func(*s3ClientConfig)

type s3ClientConfig struct {
	ctx context.Context
}

// WithContext sets the context for S3 client initialization.
func WithContext(ctx context.Context) S3Option {
	return func(c *s3ClientConfig) {
		c.ctx = ctx
	}
}

// NewS3Client creates a new S3 client from e2 credentials.
func NewS3Client(creds *labcreds.E2Credentials, opts ...S3Option) (*S3Client, error) {
	cfg := &s3ClientConfig{
		ctx: context.Background(),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Load AWS config with custom credentials
	awsCfg, err := config.LoadDefaultConfig(cfg.ctx,
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(creds.AccessKey, creds.SecretKey, ""),
		),
		config.WithRegion("us-east-1"),
	)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	// Create S3 client with custom endpoint for e2
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(creds.Endpoint)
		o.UsePathStyle = true // Required for S3-compatible services
	})

	return &S3Client{
		api:    client,
		bucket: creds.Bucket,
	}, nil
}

// newS3ClientWithAPI creates an S3Client with a custom API implementation (for testing).
func newS3ClientWithAPI(api s3API, bucket string) *S3Client {
	return &S3Client{
		api:    api,
		bucket: bucket,
	}
}

// Upload uploads a file to the S3 bucket.
func (c *S3Client) Upload(ctx context.Context, key string, body io.Reader, size int64) error {
	_, err := c.api.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return fmt.Errorf("upload to s3://%s/%s: %w", c.bucket, key, err)
	}
	return nil
}

// Download downloads a file from the S3 bucket.
func (c *S3Client) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	output, err := c.api.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("download from s3://%s/%s: %w", c.bucket, key, err)
	}
	return output.Body, nil
}

// Exists checks if an object exists in the S3 bucket.
func (c *S3Client) Exists(ctx context.Context, key string) (bool, error) {
	_, err := c.api.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// Check if it's a "not found" error
		// The AWS SDK v2 doesn't have a typed NotFound error, so we check the error message
		if isNotFoundError(err) {
			return false, nil
		}
		return false, fmt.Errorf("check existence of s3://%s/%s: %w", c.bucket, key, err)
	}
	return true, nil
}

// isNotFoundError checks if the error is an S3 not found error.
func isNotFoundError(err error) bool {
	// AWS SDK v2 returns errors that can be checked via their error code
	// For S3 HeadObject, a missing object returns a 404 status
	return err != nil && (
	// Check for common not found patterns
	contains(err.Error(), "NotFound") ||
		contains(err.Error(), "404") ||
		contains(err.Error(), "NoSuchKey"))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || s != "" && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// List lists all objects in the bucket with the given prefix.
func (c *S3Client) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	var continuationToken *string

	for {
		output, err := c.api.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, fmt.Errorf("list objects in s3://%s/%s: %w", c.bucket, prefix, err)
		}

		for _, obj := range output.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}

		if !aws.ToBool(output.IsTruncated) {
			break
		}
		continuationToken = output.NextContinuationToken
	}

	return keys, nil
}

// Delete deletes an object from the S3 bucket.
func (c *S3Client) Delete(ctx context.Context, key string) error {
	_, err := c.api.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete s3://%s/%s: %w", c.bucket, key, err)
	}
	return nil
}

// MetadataKey returns the metadata key for a given image path.
// Example: "vyos/vyos-1.5.iso" -> "metadata/vyos/vyos-1.5.iso.json"
func MetadataKey(imagePath string) string {
	return path.Join("metadata", imagePath+".json")
}

// ImageKey returns the image key for a given destination path.
// Example: "vyos/vyos-1.5.iso" -> "images/vyos/vyos-1.5.iso"
func ImageKey(destination string) string {
	return path.Join("images", destination)
}

// GetMetadata retrieves metadata for an image.
func (c *S3Client) GetMetadata(ctx context.Context, imagePath string) (*ImageMetadata, error) {
	key := MetadataKey(imagePath)

	body, err := c.Download(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	var metadata ImageMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("parse metadata: %w", err)
	}

	return &metadata, nil
}

// PutMetadata stores metadata for an image.
func (c *S3Client) PutMetadata(ctx context.Context, imagePath string, metadata *ImageMetadata) error {
	key := MetadataKey(imagePath)

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	return c.Upload(ctx, key, bytes.NewReader(data), int64(len(data)))
}

// ChecksumMatches checks if the stored metadata checksum matches the expected checksum.
func (c *S3Client) ChecksumMatches(ctx context.Context, imagePath, expectedChecksum string) (bool, error) {
	exists, err := c.Exists(ctx, MetadataKey(imagePath))
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}

	metadata, err := c.GetMetadata(ctx, imagePath)
	if err != nil {
		return false, err
	}

	return metadata.Checksum == expectedChecksum, nil
}

// GetHookResult retrieves the cached result of a hook execution.
// Returns nil, nil if the hook result doesn't exist.
func (c *S3Client) GetHookResult(ctx context.Context, imagePath, hookName string) (*HookResult, error) {
	key := HookResultKey(imagePath, hookName)

	exists, err := c.Exists(ctx, key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	body, err := c.Download(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("read hook result: %w", err)
	}

	var result HookResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse hook result: %w", err)
	}

	return &result, nil
}

// PutHookResult stores the result of a hook execution.
func (c *S3Client) PutHookResult(ctx context.Context, imagePath, hookName string, result *HookResult) error {
	key := HookResultKey(imagePath, hookName)

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal hook result: %w", err)
	}

	return c.Upload(ctx, key, bytes.NewReader(data), int64(len(data)))
}
