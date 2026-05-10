package terraformstate

import (
	"context"
	"fmt"
	"io"
	"strings"
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
// Lock metadata is observational evidence only; freshness and consistency
// decisions must be based on the opened state body and durable generation data.
type SourceMetadata struct {
	ObservedAt     time.Time
	Size           int64
	ETag           string
	LastModified   time.Time
	LockDigest     string
	LockIDHash     string
	LockObservedAt time.Time
}

// StateSource opens raw Terraform state as a bounded stream.
type StateSource interface {
	Identity() StateKey
	Open(ctx context.Context) (io.ReadCloser, SourceMetadata, error)
}

// LockMetadataClient is the consumer-side read-only lock metadata interface.
type LockMetadataClient interface {
	ReadLockMetadata(ctx context.Context, input LockMetadataInput) (LockMetadataOutput, error)
}

// LockMetadataInput identifies one Terraform state lock metadata row to read.
type LockMetadataInput struct {
	TableName string
	LockID    string
	Region    string
}

// LockMetadataOutput carries only safe lock metadata derived from a read.
type LockMetadataOutput struct {
	Digest     string
	ObservedAt time.Time
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
	Facts             []facts.Envelope
	ResourceFacts     int64
	RedactionsApplied map[string]int64
}

// Validate checks the parse context before durable fact envelopes are emitted.
func (o ParseOptions) Validate() error {
	if err := o.Scope.Validate(); err != nil {
		return fmt.Errorf("validate terraform state scope: %w", err)
	}
	if o.Scope.CollectorKind != scope.CollectorTerraformState {
		return fmt.Errorf("scope collector_kind %q must be %q", o.Scope.CollectorKind, scope.CollectorTerraformState)
	}
	if err := o.Generation.ValidateForScope(o.Scope); err != nil {
		return fmt.Errorf("validate terraform state generation: %w", err)
	}
	if strings.TrimSpace(o.Generation.FreshnessHint) == "" {
		return fmt.Errorf("terraform state generation freshness hint must not be blank")
	}
	if err := o.Source.Validate(); err != nil {
		return err
	}
	if o.FencingToken <= 0 {
		return fmt.Errorf("terraform state fencing token must be positive")
	}
	if o.RedactionKey.IsZero() {
		return fmt.Errorf("terraform state redaction key must not be empty")
	}
	return nil
}

// Validate checks the durable identity for a Terraform state source.
func (k StateKey) Validate() error {
	switch k.BackendKind {
	case BackendLocal, BackendS3, BackendTerragrunt:
	default:
		return fmt.Errorf("unsupported terraform state backend kind %q", k.BackendKind)
	}
	if strings.TrimSpace(k.Locator) == "" {
		return fmt.Errorf("terraform state source locator must not be blank")
	}
	return nil
}
