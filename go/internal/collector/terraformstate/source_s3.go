package terraformstate

import (
	"context"
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
	Bucket       string
	Key          string
	Region       string
	VersionID    string
	PreviousETag string
	MaxBytes     int64
	AllowWrite   bool
	Client       S3ObjectClient
}

// S3StateSource reads one exact S3 object through a caller-supplied client.
type S3StateSource struct {
	bucket       string
	key          string
	region       string
	versionID    string
	previousETag string
	maxBytes     int64
	client       S3ObjectClient
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

	maxBytes := config.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultStateSizeCeilingBytes
	}
	return &S3StateSource{
		bucket:       bucket,
		key:          key,
		region:       region,
		versionID:    strings.TrimSpace(config.VersionID),
		previousETag: config.PreviousETag,
		maxBytes:     maxBytes,
		client:       config.Client,
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

	return newSizeEnforcingReadCloser(output.Body, s.maxBytes), SourceMetadata{
		ObservedAt:   time.Now().UTC(),
		Size:         output.Size,
		ETag:         output.ETag,
		LastModified: output.LastModified.UTC(),
	}, nil
}

func (s *S3StateSource) safeError(err error) error {
	message := err.Error()
	for _, value := range []string{
		s.bucket,
		s.key,
		"s3://" + s.bucket + "/" + s.key,
	} {
		if strings.TrimSpace(value) != "" {
			message = strings.ReplaceAll(message, value, "<redacted>")
		}
	}
	return s3SourceError{message: message, cause: err}
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
