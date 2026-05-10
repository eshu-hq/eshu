package terraformstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// ErrStateNotModified is returned when conditional source reads find no change.
var ErrStateNotModified = errors.New("terraform state not modified")

// S3ObjectClient is the consumer-side read-only S3 object interface.
type S3ObjectClient interface {
	GetObject(ctx context.Context, input S3GetObjectInput) (S3GetObjectOutput, error)
}

// S3GetObjectInput describes the exact S3 object read request.
type S3GetObjectInput struct {
	Bucket      string
	Key         string
	Region      string
	VersionID   string
	IfNoneMatch string
}

// S3GetObjectOutput describes a read-only S3 object response.
type S3GetObjectOutput struct {
	Body         io.ReadCloser
	Size         int64
	ETag         string
	LastModified time.Time
}

// S3SourceConfig configures one exact read-only S3 state object source.
type S3SourceConfig struct {
	Bucket        string
	Key           string
	Region        string
	VersionID     string
	PreviousETag  string
	MaxBytes      int64
	AllowWrite    bool
	Client        S3ObjectClient
	LockTableName string
	LockID        string
	LockClient    LockMetadataClient
}

// S3StateSource reads one exact S3 object through a caller-supplied client.
type S3StateSource struct {
	bucket        string
	key           string
	region        string
	versionID     string
	previousETag  string
	maxBytes      int64
	client        S3ObjectClient
	lockTableName string
	lockID        string
	lockClient    LockMetadataClient
}

// NewS3StateSource validates and returns a read-only S3 Terraform state source.
func NewS3StateSource(config S3SourceConfig) (*S3StateSource, error) {
	bucket := strings.TrimSpace(config.Bucket)
	key := strings.TrimSpace(config.Key)
	region := strings.TrimSpace(config.Region)
	if bucket == "" {
		return nil, fmt.Errorf("s3 state bucket must not be blank")
	}
	if key == "" {
		return nil, fmt.Errorf("s3 state key must not be blank")
	}
	if strings.HasSuffix(key, "/") {
		return nil, fmt.Errorf("s3 state key must name an exact object")
	}
	if region == "" {
		return nil, fmt.Errorf("s3 state region must not be blank")
	}
	if config.AllowWrite {
		return nil, fmt.Errorf("s3 state source must be read-only")
	}
	if config.Client == nil {
		return nil, fmt.Errorf("s3 state client must not be nil")
	}
	lockConfig, err := validateS3LockConfig(config)
	if err != nil {
		return nil, err
	}

	maxBytes := config.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultStateSizeCeilingBytes
	}
	return &S3StateSource{
		bucket:        bucket,
		key:           key,
		region:        region,
		versionID:     strings.TrimSpace(config.VersionID),
		previousETag:  config.PreviousETag,
		maxBytes:      maxBytes,
		client:        config.Client,
		lockTableName: lockConfig.tableName,
		lockID:        lockConfig.lockID,
		lockClient:    lockConfig.client,
	}, nil
}

// Identity returns the durable source identity for this exact S3 object.
func (s *S3StateSource) Identity() StateKey {
	if s == nil {
		return StateKey{BackendKind: BackendS3}
	}
	return StateKey{
		BackendKind: BackendS3,
		Locator:     "s3://" + s.bucket + "/" + s.key,
		VersionID:   s.versionID,
	}
}

// Open opens the exact S3 object with the configured conditional ETag.
func (s *S3StateSource) Open(ctx context.Context) (io.ReadCloser, SourceMetadata, error) {
	if s == nil {
		return nil, SourceMetadata{}, fmt.Errorf("s3 state source is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, SourceMetadata{}, err
	}

	output, err := s.client.GetObject(ctx, S3GetObjectInput{
		Bucket:      s.bucket,
		Key:         s.key,
		Region:      s.region,
		VersionID:   s.versionID,
		IfNoneMatch: s.previousETag,
	})
	if err != nil {
		if errors.Is(err, ErrStateNotModified) {
			return nil, SourceMetadata{}, err
		}
		return nil, SourceMetadata{}, s.safeError(err)
	}
	if output.Body == nil {
		return nil, SourceMetadata{}, fmt.Errorf("s3 state response body must not be nil")
	}
	if output.Size > s.maxBytes {
		_ = output.Body.Close()
		return nil, SourceMetadata{}, fmt.Errorf("%w: size=%d max=%d", ErrStateTooLarge, output.Size, s.maxBytes)
	}

	metadata := SourceMetadata{
		ObservedAt:   time.Now().UTC(),
		Size:         output.Size,
		ETag:         output.ETag,
		LastModified: output.LastModified.UTC(),
	}
	if s.lockClient != nil {
		lockMetadata, err := s.lockClient.ReadLockMetadata(ctx, LockMetadataInput{
			TableName: s.lockTableName,
			LockID:    s.lockID,
			Region:    s.region,
		})
		if err != nil {
			_ = output.Body.Close()
			return nil, SourceMetadata{}, s.safeError(err)
		}
		metadata.LockDigest = lockMetadata.Digest
		metadata.LockIDHash = hashLockID(s.lockID)
		metadata.LockObservedAt = firstNonZeroTime(lockMetadata.ObservedAt, time.Now().UTC())
	}

	return newSizeEnforcingReadCloser(output.Body, s.maxBytes), metadata, nil
}

func (s *S3StateSource) safeError(err error) error {
	message := err.Error()
	for _, value := range []string{
		s.lockID,
		"s3://" + s.bucket + "/" + s.key,
		s.key,
		s.bucket,
		s.lockTableName,
	} {
		if strings.TrimSpace(value) != "" {
			message = strings.ReplaceAll(message, value, "<redacted>")
		}
	}
	return s3SourceError{message: message, cause: safeS3SourceCause(err)}
}

type s3SourceError struct {
	message string
	cause   error
}

func (e s3SourceError) Error() string {
	return e.message
}

func (e s3SourceError) Unwrap() error {
	return e.cause
}

func safeS3SourceCause(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, ErrStateNotModified) || errors.Is(err, ErrStateTooLarge) {
		return err
	}
	return nil
}

type s3LockConfig struct {
	tableName string
	lockID    string
	client    LockMetadataClient
}

func validateS3LockConfig(config S3SourceConfig) (s3LockConfig, error) {
	tableName := strings.TrimSpace(config.LockTableName)
	lockID := strings.TrimSpace(config.LockID)
	client := config.LockClient
	if tableName == "" && lockID == "" && client == nil {
		return s3LockConfig{}, nil
	}
	if tableName == "" {
		return s3LockConfig{}, fmt.Errorf("s3 state lock table name must not be blank")
	}
	if lockID == "" {
		return s3LockConfig{}, fmt.Errorf("s3 state lock id must not be blank")
	}
	if client == nil {
		return s3LockConfig{}, fmt.Errorf("s3 state lock metadata client must not be nil")
	}
	return s3LockConfig{
		tableName: tableName,
		lockID:    lockID,
		client:    client,
	}, nil
}

func hashLockID(lockID string) string {
	sum := sha256.Sum256([]byte(lockID))
	return hex.EncodeToString(sum[:])
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Now().UTC()
}
