package images

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/GilmanLab/lab/tools/labctl/internal/store"
)

// mockStoreClient implements store.Client for testing.
type mockStoreClient struct {
	uploadFunc        func(ctx context.Context, key string, body io.Reader, size int64) error
	downloadFunc      func(ctx context.Context, key string) (io.ReadCloser, error)
	existsFunc        func(ctx context.Context, key string) (bool, error)
	listFunc          func(ctx context.Context, prefix string) ([]string, error)
	deleteFunc        func(ctx context.Context, key string) error
	getMetadataFunc   func(ctx context.Context, imagePath string) (*store.ImageMetadata, error)
	putMetadataFunc   func(ctx context.Context, imagePath string, metadata *store.ImageMetadata) error
	checksumMatchFunc func(ctx context.Context, imagePath, expectedChecksum string) (bool, error)
	uploadedKeys      []string
	deletedKeys       []string
	putMetadataCalls  []*store.ImageMetadata
}

func (m *mockStoreClient) Upload(ctx context.Context, key string, body io.Reader, size int64) error {
	m.uploadedKeys = append(m.uploadedKeys, key)
	if m.uploadFunc != nil {
		return m.uploadFunc(ctx, key, body, size)
	}
	return nil
}

func (m *mockStoreClient) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	if m.downloadFunc != nil {
		return m.downloadFunc(ctx, key)
	}
	return nil, errors.New("not implemented")
}

func (m *mockStoreClient) Exists(ctx context.Context, key string) (bool, error) {
	if m.existsFunc != nil {
		return m.existsFunc(ctx, key)
	}
	return false, nil
}

func (m *mockStoreClient) List(ctx context.Context, prefix string) ([]string, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, prefix)
	}
	return nil, nil
}

func (m *mockStoreClient) Delete(ctx context.Context, key string) error {
	m.deletedKeys = append(m.deletedKeys, key)
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, key)
	}
	return nil
}

func (m *mockStoreClient) GetMetadata(ctx context.Context, imagePath string) (*store.ImageMetadata, error) {
	if m.getMetadataFunc != nil {
		return m.getMetadataFunc(ctx, imagePath)
	}
	return &store.ImageMetadata{
		Name:       "test-image",
		Checksum:   "sha256:abc123",
		Size:       1024,
		UploadedAt: time.Now(),
	}, nil
}

func (m *mockStoreClient) PutMetadata(ctx context.Context, imagePath string, metadata *store.ImageMetadata) error {
	m.putMetadataCalls = append(m.putMetadataCalls, metadata)
	if m.putMetadataFunc != nil {
		return m.putMetadataFunc(ctx, imagePath, metadata)
	}
	return nil
}

func (m *mockStoreClient) ChecksumMatches(ctx context.Context, imagePath, expectedChecksum string) (bool, error) {
	if m.checksumMatchFunc != nil {
		return m.checksumMatchFunc(ctx, imagePath, expectedChecksum)
	}
	return false, nil
}

func (*mockStoreClient) GetHookResult(_ context.Context, _, _ string) (*store.HookResult, error) {
	return nil, nil
}

func (*mockStoreClient) PutHookResult(_ context.Context, _, _ string, _ *store.HookResult) error {
	return nil
}
