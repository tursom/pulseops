package store

import (
	"context"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"pulseops/internal/config"
)

func TestMinIOArtifactStorePutGeneratesMetadata(t *testing.T) {
	t.Parallel()

	client := &fakeObjectClient{}
	st := &MinIOArtifactStore{
		bucket:     "pulseops-artifacts",
		basePath:   "prod",
		kind:       "s3",
		presignTTL: 15 * time.Minute,
		client:     client,
	}

	ref, err := st.Put(context.Background(), "prod/task-a/2026/04/27/run-1/payload.json", strings.NewReader(`{"hello":"world"}`), ArtifactMeta{
		Kind: "payload",
	})
	if err != nil {
		t.Fatalf("put artifact: %v", err)
	}
	if ref.StorageKind != "s3" || ref.Kind != "payload" {
		t.Fatalf("unexpected artifact ref: %#v", ref)
	}
	if ref.URI != "s3://pulseops-artifacts/prod/task-a/2026/04/27/run-1/payload.json" {
		t.Fatalf("unexpected uri: %q", ref.URI)
	}
	if ref.SHA256 == "" || ref.SizeBytes == 0 {
		t.Fatalf("expected sha256 and size to be populated: %#v", ref)
	}
}

func TestMinIOArtifactStorePresignGetUsesConfiguredTTL(t *testing.T) {
	t.Parallel()

	client := &fakeObjectClient{
		presignedURL: "https://download.local/object",
	}
	st := &MinIOArtifactStore{
		bucket:     "pulseops-artifacts",
		kind:       "s3",
		presignTTL: 15 * time.Minute,
		client:     client,
	}

	downloadURL, err := st.PresignGet(context.Background(), "prod/task-a/object.json", 0)
	if err != nil {
		t.Fatalf("presign get: %v", err)
	}
	if downloadURL != "https://download.local/object" {
		t.Fatalf("unexpected download url %q", downloadURL)
	}
	if client.lastPresignTTL != 15*time.Minute {
		t.Fatalf("expected default ttl, got %s", client.lastPresignTTL)
	}
}

func TestNewMinIOArtifactStoreRejectsMissingCoreConfig(t *testing.T) {
	t.Parallel()

	if _, err := NewMinIOArtifactStore(config.ArtifactStoreConfig{
		Endpoint: "http://127.0.0.1:9000",
	}); err == nil || !strings.Contains(err.Error(), "artifact_store.bucket is required") {
		t.Fatalf("expected missing bucket error, got %v", err)
	}
	if _, err := NewMinIOArtifactStore(config.ArtifactStoreConfig{
		Bucket: "pulseops-artifacts",
	}); err == nil || !strings.Contains(err.Error(), "artifact_store.endpoint is required") {
		t.Fatalf("expected missing endpoint error, got %v", err)
	}
}

func TestObjectKeyHelpers(t *testing.T) {
	t.Parallel()

	st := &MinIOArtifactStore{basePath: "prod"}
	key := st.BuildObjectKey("task-a", "run-1", "payload.json", time.Date(2026, 4, 27, 1, 0, 0, 0, time.UTC))
	if key != "prod/task-a/2026/04/27/run-1/payload.json" {
		t.Fatalf("unexpected key %q", key)
	}
	parsed, err := ObjectKeyFromURI("s3://pulseops-artifacts/" + key)
	if err != nil {
		t.Fatalf("parse object key from uri: %v", err)
	}
	if parsed != key {
		t.Fatalf("expected parsed key %q, got %q", key, parsed)
	}
}

type fakeObjectClient struct {
	lastObjectKey  string
	lastBody       string
	lastPresignTTL time.Duration
	presignedURL   string
}

func (c *fakeObjectClient) BucketExists(context.Context, string) (bool, error) { return true, nil }
func (c *fakeObjectClient) MakeBucket(context.Context, string, minio.MakeBucketOptions) error {
	return nil
}

func (c *fakeObjectClient) PutObject(_ context.Context, _ string, objectName string, reader io.Reader, _ int64, _ minio.PutObjectOptions) (minio.UploadInfo, error) {
	raw, _ := io.ReadAll(reader)
	c.lastObjectKey = objectName
	c.lastBody = string(raw)
	return minio.UploadInfo{Key: objectName, Size: int64(len(raw))}, nil
}

func (c *fakeObjectClient) GetObject(context.Context, string, string, minio.GetObjectOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(c.lastBody)), nil
}

func (c *fakeObjectClient) PresignedGetObject(_ context.Context, _ string, objectName string, expires time.Duration, _ url.Values) (*url.URL, error) {
	c.lastObjectKey = objectName
	c.lastPresignTTL = expires
	return url.Parse(c.presignedURL)
}

func (c *fakeObjectClient) RemoveObject(context.Context, string, string, minio.RemoveObjectOptions) error {
	return nil
}
