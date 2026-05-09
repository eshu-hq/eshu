package terraformstate

import (
	"context"
	"io"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const defaultStateSizeCeilingBytes int64 = 512 * 1024 * 1024

// BackendKind identifies the Terraform state backend family.
type BackendKind string

const (
	// BackendLocal identifies an exact operator-approved local state file.
	BackendLocal BackendKind = "local"
	// BackendS3 identifies an exact S3 object state backend.
	BackendS3 BackendKind = "s3"
	// BackendTerragrunt identifies a Terragrunt source that resolves to another backend.
	BackendTerragrunt BackendKind = "terragrunt"
)

// StateKey identifies one exact state snapshot source.
type StateKey struct {
	BackendKind BackendKind
	Locator     string
	VersionID   string
}

// SourceMetadata describes the opened source stream without carrying raw bytes.
type SourceMetadata struct {
	ObservedAt   time.Time
	Size         int64
	ETag         string
	LastModified time.Time
}

// StateSource opens raw Terraform state as a bounded stream.
type StateSource interface {
	Identity() StateKey
	Open(ctx context.Context) (io.ReadCloser, SourceMetadata, error)
}

// ParseOptions carries the durable envelope and redaction context for parsing.
type ParseOptions struct {
	Scope          scope.IngestionScope
	Generation     scope.ScopeGeneration
	Source         StateKey
	Metadata       SourceMetadata
	ObservedAt     time.Time
	RedactionKey   redact.Key
	RedactionRules redact.RuleSet
	FencingToken   int64
}

// ParseResult is the redacted fact output from one Terraform state parse.
type ParseResult struct {
	Facts []facts.Envelope
}
