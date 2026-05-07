package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"pulseops/internal/config"
)

type ArtifactMeta struct {
	TaskID       string
	RunID        string
	ArtifactName string
	Kind         string
	ContentType  string
	PreviewText  string
}

type ArtifactStore interface {
	Kind() string
	Put(ctx context.Context, key string, body io.Reader, meta ArtifactMeta) (ArtifactRef, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	Delete(ctx context.Context, key string) error
}

type objectClient interface {
	BucketExists(ctx context.Context, bucketName string) (bool, error)
	MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error
	PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error)
	GetObject(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) (io.ReadCloser, error)
	PresignedGetObject(ctx context.Context, bucketName, objectName string, expires time.Duration, reqParams url.Values) (*url.URL, error)
	RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error
}

type minioClientAdapter struct {
	client *minio.Client
}

func (a minioClientAdapter) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	return a.client.BucketExists(ctx, bucketName)
}

func (a minioClientAdapter) MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error {
	return a.client.MakeBucket(ctx, bucketName, opts)
}

func (a minioClientAdapter) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	return a.client.PutObject(ctx, bucketName, objectName, reader, objectSize, opts)
}

func (a minioClientAdapter) GetObject(ctx context.Context, bucketName, objectName string, opts minio.GetObjectOptions) (io.ReadCloser, error) {
	return a.client.GetObject(ctx, bucketName, objectName, opts)
}

func (a minioClientAdapter) PresignedGetObject(ctx context.Context, bucketName, objectName string, expires time.Duration, reqParams url.Values) (*url.URL, error) {
	return a.client.PresignedGetObject(ctx, bucketName, objectName, expires, reqParams)
}

func (a minioClientAdapter) RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error {
	return a.client.RemoveObject(ctx, bucketName, objectName, opts)
}

type MinIOArtifactStore struct {
	bucket     string
	basePath   string
	kind       string
	presignTTL time.Duration
	client     objectClient
}

func NewMinIOArtifactStore(cfg config.ArtifactStoreConfig) (*MinIOArtifactStore, error) {
	endpoint, secure, err := resolveEndpoint(cfg)
	if err != nil {
		return nil, err
	}
	bucketLookup := minio.BucketLookupAuto
	if cfg.ForcePathStyle {
		bucketLookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure:       secure,
		Region:       cfg.Region,
		BucketLookup: bucketLookup,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}
	store := &MinIOArtifactStore{
		bucket:     cfg.Bucket,
		basePath:   strings.Trim(cfg.BasePath, "/"),
		kind:       "s3",
		presignTTL: cfg.PresignTTL.Duration,
		client:     minioClientAdapter{client: client},
	}
	if err := store.ensureBucket(context.Background(), cfg.Region); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *MinIOArtifactStore) Kind() string {
	return s.kind
}

func (s *MinIOArtifactStore) Put(ctx context.Context, key string, body io.Reader, meta ArtifactMeta) (ArtifactRef, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return ArtifactRef{}, fmt.Errorf("read artifact body: %w", err)
	}
	sum := sha256.Sum256(raw)
	contentType := meta.ContentType
	if contentType == "" {
		contentType = http.DetectContentType(raw)
	}
	previewText := meta.PreviewText
	if previewText == "" {
		previewText = derivePreviewText(raw, contentType)
	}
	if _, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(raw), int64(len(raw)), minio.PutObjectOptions{
		ContentType: contentType,
	}); err != nil {
		return ArtifactRef{}, fmt.Errorf("put artifact object: %w", err)
	}
	return ArtifactRef{
		ArtifactID:  uuid.NewString(),
		Kind:        meta.Kind,
		StorageKind: s.kind,
		URI:         fmt.Sprintf("s3://%s/%s", s.bucket, key),
		ContentType: contentType,
		SizeBytes:   int64(len(raw)),
		SHA256:      hex.EncodeToString(sum[:]),
		PreviewText: previewText,
	}, nil
}

func (s *MinIOArtifactStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
}

func (s *MinIOArtifactStore) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = s.presignTTL
	}
	presigned, err := s.client.PresignedGetObject(ctx, s.bucket, key, ttl, nil)
	if err != nil {
		return "", fmt.Errorf("presign artifact: %w", err)
	}
	return presigned.String(), nil
}

func (s *MinIOArtifactStore) Delete(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	return nil
}

func (s *MinIOArtifactStore) BuildObjectKey(taskID, runID, artifactName string, startedAt time.Time) string {
	parts := []string{}
	if s.basePath != "" {
		parts = append(parts, s.basePath)
	}
	parts = append(parts, taskID, startedAt.Format("2006/01/02"), runID, artifactName)
	return path.Join(parts...)
}

func ObjectKeyFromURI(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse artifact uri: %w", err)
	}
	if parsed.Scheme != "s3" {
		return "", fmt.Errorf("unsupported artifact uri scheme %q", parsed.Scheme)
	}
	key := strings.TrimPrefix(parsed.Path, "/")
	if key == "" {
		return "", fmt.Errorf("artifact uri missing key")
	}
	return key, nil
}

func derivePreviewText(raw []byte, contentType string) string {
	if strings.Contains(contentType, "json") || strings.HasPrefix(contentType, "text/") {
		text := string(raw)
		if len(text) > 256 {
			return text[:256]
		}
		return text
	}
	return ""
}

func resolveEndpoint(cfg config.ArtifactStoreConfig) (string, bool, error) {
	if strings.HasPrefix(cfg.Endpoint, "http://") || strings.HasPrefix(cfg.Endpoint, "https://") {
		parsed, err := url.Parse(cfg.Endpoint)
		if err != nil {
			return "", false, fmt.Errorf("parse artifact endpoint: %w", err)
		}
		return parsed.Host, parsed.Scheme == "https", nil
	}
	return cfg.Endpoint, cfg.UseSSL, nil
}

func (s *MinIOArtifactStore) ensureBucket(ctx context.Context, region string) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check artifact bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: region}); err != nil {
		return fmt.Errorf("create artifact bucket: %w", err)
	}
	return nil
}
