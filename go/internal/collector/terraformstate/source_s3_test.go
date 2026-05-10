package terraformstate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
)

func TestS3StateSourceOpensExactObjectWithConditionalETag(t *testing.T) {
	t.Parallel()

	client := &recordingS3Client{
		output: terraformstate.S3GetObjectOutput{
			Body:         io.NopCloser(strings.NewReader(`{"serial":17}`)),
			Size:         13,
			ETag:         `"etag-456"`,
			LastModified: time.Date(2026, time.May, 9, 17, 0, 0, 0, time.UTC),
		},
	}
	source, err := terraformstate.NewS3StateSource(terraformstate.S3SourceConfig{
		Bucket:       "tfstate-prod",
		Key:          "services/api/terraform.tfstate",
		Region:       "us-east-1",
		PreviousETag: `"etag-123"`,
		MaxBytes:     1024,
		Client:       client,
	})
	if err != nil {
		t.Fatalf("NewS3StateSource() error = %v, want nil", err)
	}

	reader, metadata, err := source.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v, want nil", err)
	}
	defer closeReader(t, reader)

	if got, want := client.input.Bucket, "tfstate-prod"; got != want {
		t.Fatalf("Bucket = %q, want %q", got, want)
	}
	if got, want := client.input.Key, "services/api/terraform.tfstate"; got != want {
		t.Fatalf("Key = %q, want %q", got, want)
	}
	if got, want := client.input.IfNoneMatch, `"etag-123"`; got != want {
		t.Fatalf("IfNoneMatch = %q, want %q", got, want)
	}
	if got, want := source.Identity().Locator, "s3://tfstate-prod/services/api/terraform.tfstate"; got != want {
		t.Fatalf("Locator = %q, want %q", got, want)
	}
	if got, want := metadata.ETag, `"etag-456"`; got != want {
		t.Fatalf("metadata.ETag = %q, want %q", got, want)
	}
}

func TestS3StateSourceKeepsETagsOpaque(t *testing.T) {
	t.Parallel()

	priorETag := " \t\"etag-123\"\t "
	responseETag := " \t\"etag-456\"\t "
	client := &recordingS3Client{
		output: terraformstate.S3GetObjectOutput{
			Body: io.NopCloser(strings.NewReader(`{"serial":17}`)),
			Size: 13,
			ETag: responseETag,
		},
	}
	source, err := terraformstate.NewS3StateSource(terraformstate.S3SourceConfig{
		Bucket:       "tfstate-prod",
		Key:          "services/api/terraform.tfstate",
		Region:       "us-east-1",
		PreviousETag: priorETag,
		Client:       client,
	})
	if err != nil {
		t.Fatalf("NewS3StateSource() error = %v, want nil", err)
	}

	reader, metadata, err := source.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v, want nil", err)
	}
	defer closeReader(t, reader)

	if got := client.input.IfNoneMatch; got != priorETag {
		t.Fatalf("IfNoneMatch = %q, want opaque prior ETag %q", got, priorETag)
	}
	if got := metadata.ETag; got != responseETag {
		t.Fatalf("metadata.ETag = %q, want opaque response ETag %q", got, responseETag)
	}
}

func TestS3StateSourceRejectsUnsafeOrInexactConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config terraformstate.S3SourceConfig
	}{
		{
			name:   "blank bucket",
			config: terraformstate.S3SourceConfig{Key: "state.tfstate", Region: "us-east-1", Client: &recordingS3Client{}},
		},
		{
			name:   "blank key",
			config: terraformstate.S3SourceConfig{Bucket: "tfstate-prod", Region: "us-east-1", Client: &recordingS3Client{}},
		},
		{
			name:   "prefix key",
			config: terraformstate.S3SourceConfig{Bucket: "tfstate-prod", Key: "services/api/", Region: "us-east-1", Client: &recordingS3Client{}},
		},
		{
			name:   "blank region",
			config: terraformstate.S3SourceConfig{Bucket: "tfstate-prod", Key: "state.tfstate", Client: &recordingS3Client{}},
		},
		{
			name:   "write access requested",
			config: terraformstate.S3SourceConfig{Bucket: "tfstate-prod", Key: "state.tfstate", Region: "us-east-1", AllowWrite: true, Client: &recordingS3Client{}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := terraformstate.NewS3StateSource(test.config)
			if err == nil {
				t.Fatal("NewS3StateSource() error = nil, want non-nil")
			}
		})
	}
}

func TestS3StateSourceReportsNotModified(t *testing.T) {
	t.Parallel()

	lockClient := &recordingLockMetadataClient{}
	source, err := terraformstate.NewS3StateSource(terraformstate.S3SourceConfig{
		Bucket:        "tfstate-prod",
		Key:           "state.tfstate",
		Region:        "us-east-1",
		PreviousETag:  `"etag-123"`,
		Client:        &recordingS3Client{err: terraformstate.ErrStateNotModified},
		LockTableName: "tfstate-locks",
		LockID:        "tfstate-prod/state.tfstate-md5",
		LockClient:    lockClient,
	})
	if err != nil {
		t.Fatalf("NewS3StateSource() error = %v, want nil", err)
	}

	_, _, err = source.Open(context.Background())
	if !errors.Is(err, terraformstate.ErrStateNotModified) {
		t.Fatalf("Open() error = %v, want ErrStateNotModified", err)
	}
	if got := lockClient.calls; got != 0 {
		t.Fatalf("lock metadata calls = %d, want 0 for not-modified state", got)
	}
}

func TestS3StateSourceCopiesSafeDynamoDBLockMetadata(t *testing.T) {
	t.Parallel()

	lockObservedAt := time.Date(2026, time.May, 10, 14, 0, 0, 0, time.UTC)
	lockClient := &recordingLockMetadataClient{
		output: terraformstate.LockMetadataOutput{
			Digest:     "6f7c1bd9b803d5ec",
			ObservedAt: lockObservedAt,
		},
	}
	source, err := terraformstate.NewS3StateSource(terraformstate.S3SourceConfig{
		Bucket:        "tfstate-prod",
		Key:           "services/api/terraform.tfstate",
		Region:        "us-east-1",
		Client:        &recordingS3Client{output: terraformstate.S3GetObjectOutput{Body: io.NopCloser(strings.NewReader(`{"serial":17}`)), Size: 13}},
		LockTableName: "tfstate-locks",
		LockID:        "tfstate-prod/services/api/terraform.tfstate-md5",
		LockClient:    lockClient,
	})
	if err != nil {
		t.Fatalf("NewS3StateSource() error = %v, want nil", err)
	}

	reader, metadata, err := source.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v, want nil", err)
	}
	defer closeReader(t, reader)

	if got, want := lockClient.input.TableName, "tfstate-locks"; got != want {
		t.Fatalf("lock input TableName = %q, want %q", got, want)
	}
	if got, want := lockClient.input.LockID, "tfstate-prod/services/api/terraform.tfstate-md5"; got != want {
		t.Fatalf("lock input LockID = %q, want %q", got, want)
	}
	if got, want := lockClient.input.Region, "us-east-1"; got != want {
		t.Fatalf("lock input Region = %q, want %q", got, want)
	}
	if got, want := metadata.LockDigest, "6f7c1bd9b803d5ec"; got != want {
		t.Fatalf("metadata.LockDigest = %q, want %q", got, want)
	}
	if got, want := metadata.LockIDHash, lockIDHash("tfstate-prod/services/api/terraform.tfstate-md5"); got != want {
		t.Fatalf("metadata.LockIDHash = %q, want %q", got, want)
	}
	if strings.Contains(metadata.LockIDHash, "tfstate-prod") || strings.Contains(metadata.LockIDHash, "terraform.tfstate") {
		t.Fatalf("metadata.LockIDHash = %q, must not include raw lock ID", metadata.LockIDHash)
	}
	if got := metadata.LockObservedAt; !got.Equal(lockObservedAt) {
		t.Fatalf("metadata.LockObservedAt = %v, want %v", got, lockObservedAt)
	}
}

func TestS3StateSourceRejectsIncompleteDynamoDBLockConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config terraformstate.S3SourceConfig
	}{
		{
			name:   "table without lock id",
			config: terraformstate.S3SourceConfig{LockTableName: "tfstate-locks", LockClient: &recordingLockMetadataClient{}},
		},
		{
			name:   "table without client",
			config: terraformstate.S3SourceConfig{LockTableName: "tfstate-locks", LockID: "tfstate-prod/state.tfstate-md5"},
		},
		{
			name:   "lock id without table",
			config: terraformstate.S3SourceConfig{LockID: "tfstate-prod/state.tfstate-md5", LockClient: &recordingLockMetadataClient{}},
		},
		{
			name:   "client without table",
			config: terraformstate.S3SourceConfig{LockClient: &recordingLockMetadataClient{}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := test.config
			config.Bucket = "tfstate-prod"
			config.Key = "state.tfstate"
			config.Region = "us-east-1"
			config.Client = &recordingS3Client{}
			_, err := terraformstate.NewS3StateSource(config)
			if err == nil {
				t.Fatal("NewS3StateSource() error = nil, want incomplete lock config error")
			}
		})
	}
}

func TestS3StateSourceRedactsDynamoDBLockConfigFromClientError(t *testing.T) {
	t.Parallel()

	rawErr := errors.New("get item from table tfstate-locks lock tfstate-prod/state.tfstate-md5 failed")
	source, err := terraformstate.NewS3StateSource(terraformstate.S3SourceConfig{
		Bucket:        "tfstate-prod",
		Key:           "state.tfstate",
		Region:        "us-east-1",
		Client:        &recordingS3Client{output: terraformstate.S3GetObjectOutput{Body: io.NopCloser(strings.NewReader(`{"serial":17}`)), Size: 13}},
		LockTableName: "tfstate-locks",
		LockID:        "tfstate-prod/state.tfstate-md5",
		LockClient:    &recordingLockMetadataClient{err: rawErr},
	})
	if err != nil {
		t.Fatalf("NewS3StateSource() error = %v, want nil", err)
	}

	_, _, err = source.Open(context.Background())
	if errors.Is(err, rawErr) {
		t.Fatalf("Open() error unwraps raw lock error %v, want sanitized source error only", err)
	}
	for _, leaked := range []string{"tfstate-locks", "tfstate-prod/state.tfstate-md5", "state.tfstate"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("Open() error = %q, must not leak %q", err.Error(), leaked)
		}
	}
}

func TestS3StateSourceRedactsBucketAndKeyFromClientError(t *testing.T) {
	t.Parallel()

	rawErr := errors.New("GET bucket tfstate-prod key services/api/terraform.tfstate failed")
	source, err := terraformstate.NewS3StateSource(terraformstate.S3SourceConfig{
		Bucket: "tfstate-prod",
		Key:    "services/api/terraform.tfstate",
		Region: "us-east-1",
		Client: &recordingS3Client{err: rawErr},
	})
	if err != nil {
		t.Fatalf("NewS3StateSource() error = %v, want nil", err)
	}

	_, _, err = source.Open(context.Background())
	if errors.Is(err, rawErr) {
		t.Fatalf("Open() error unwraps raw S3 error %v, want sanitized source error only", err)
	}
	for _, leaked := range []string{"tfstate-prod", "services/api/terraform.tfstate"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("Open() error = %q, must not leak %q", err.Error(), leaked)
		}
	}
}

func TestS3StateSourcePreservesContextCancellationClass(t *testing.T) {
	t.Parallel()

	source, err := terraformstate.NewS3StateSource(terraformstate.S3SourceConfig{
		Bucket: "tfstate-prod",
		Key:    "services/api/terraform.tfstate",
		Region: "us-east-1",
		Client: &recordingS3Client{err: context.Canceled},
	})
	if err != nil {
		t.Fatalf("NewS3StateSource() error = %v, want nil", err)
	}

	_, _, err = source.Open(context.Background())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Open() error = %v, want context.Canceled classification", err)
	}
}

func TestS3StateSourceEnforcesSizeCeilingWhileReading(t *testing.T) {
	t.Parallel()

	source, err := terraformstate.NewS3StateSource(terraformstate.S3SourceConfig{
		Bucket:   "tfstate-prod",
		Key:      "state.tfstate",
		Region:   "us-east-1",
		MaxBytes: 4,
		Client: &recordingS3Client{
			output: terraformstate.S3GetObjectOutput{
				Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 8))),
				Size: 4,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewS3StateSource() error = %v, want nil", err)
	}

	reader, _, err := source.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v, want nil", err)
	}
	defer closeReader(t, reader)

	_, err = io.ReadAll(reader)
	if !errors.Is(err, terraformstate.ErrStateTooLarge) {
		t.Fatalf("ReadAll() error = %v, want ErrStateTooLarge", err)
	}
}

type recordingS3Client struct {
	input  terraformstate.S3GetObjectInput
	output terraformstate.S3GetObjectOutput
	err    error
}

func (c *recordingS3Client) GetObject(ctx context.Context, input terraformstate.S3GetObjectInput) (terraformstate.S3GetObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return terraformstate.S3GetObjectOutput{}, err
	}
	c.input = input
	if c.err != nil {
		return terraformstate.S3GetObjectOutput{}, c.err
	}
	return c.output, nil
}

type recordingLockMetadataClient struct {
	input  terraformstate.LockMetadataInput
	output terraformstate.LockMetadataOutput
	err    error
	calls  int
}

func (c *recordingLockMetadataClient) ReadLockMetadata(
	ctx context.Context,
	input terraformstate.LockMetadataInput,
) (terraformstate.LockMetadataOutput, error) {
	if err := ctx.Err(); err != nil {
		return terraformstate.LockMetadataOutput{}, err
	}
	c.calls++
	c.input = input
	if c.err != nil {
		return terraformstate.LockMetadataOutput{}, c.err
	}
	return c.output, nil
}

func lockIDHash(lockID string) string {
	sum := sha256.Sum256([]byte(lockID))
	return hex.EncodeToString(sum[:])
}
