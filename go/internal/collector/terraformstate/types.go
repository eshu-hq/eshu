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

// ProviderSchemaResolver answers whether a Terraform resource attribute is
// covered by a loaded provider schema. Callers that supply a non-nil resolver
// authorize the parser to mark covered attributes as redact.SchemaKnown so
// non-sensitive scalars flow through to downstream drift detection unredacted.
// A nil resolver, or a resolver that does not know a given (resourceType,
// attributeKey) pair, fails closed via redact.SchemaUnknown.
//
// Implementations must be safe for concurrent use because one resolver is
// shared across every Terraform-state parse the collector runs.
type ProviderSchemaResolver interface {
	HasAttribute(resourceType string, attributeKey string) bool
}

// CompositeCaptureSkip describes one moment where the streaming nested walker
// stopped walking a SchemaKnown Terraform-state composite because the state
// JSON shape disagreed with the schema's expectation. Carries the diagnostic
// detail (path, attribute key, parse error) operators need to investigate a
// "bundle and state disagree" condition without exposing high-cardinality
// dimensions through metric labels.
type CompositeCaptureSkip struct {
	ResourceType string
	AttributeKey string
	Path         string
	Err          error
}

// CompositeCaptureRecorder is the observability seam the streaming nested
// walker uses when it stops walking a SchemaKnown composite. Implementations
// must be safe for concurrent use; one recorder is shared across every
// Terraform-state parse the collector runs.
//
// The collector wires a recorder that increments the
// eshu_dp_drift_schema_unknown_composite_total{resource_type} counter and
// emits a slog.Warn line with the high-cardinality attribute_key and source
// path. A nil recorder is allowed (early bootstrap, fixtures without
// telemetry); the parser treats nil as a no-op.
type CompositeCaptureRecorder interface {
	Record(ctx context.Context, skip CompositeCaptureSkip)
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
	// SchemaResolver authorizes the parser to mark covered Terraform-state
	// attributes as redact.SchemaKnown. Without a resolver, every attribute
	// stays redact.SchemaUnknown which fails closed under the configured
	// RedactionRules. The collector wires a real resolver from the packaged
	// terraformschema bundle at startup; tests inject deterministic stubs.
	SchemaResolver ProviderSchemaResolver
	// CompositeCaptureMetrics receives one Record call every time the
	// streaming nested walker stops walking a SchemaKnown composite because
	// the state JSON shape disagrees with the schema. A nil recorder is
	// treated as a no-op so fixtures and early bootstrap stay operable.
	CompositeCaptureMetrics CompositeCaptureRecorder
	FencingToken            int64
	SourceWarnings          []SourceWarning
}

// SourceWarning is source-level evidence that should be emitted with the parse
// result without exposing raw Terraform state bytes or locators.
type SourceWarning struct {
	WarningKind string
	Reason      string
	Source      string
}

// FactSink receives redacted Terraform-state fact envelopes produced by
// ParseStream. Implementations must not retain facts unless they intentionally
// want collection semantics.
type FactSink interface {
	Emit(context.Context, facts.Envelope) error
}

// FactSinkFunc adapts a function into a FactSink.
type FactSinkFunc func(context.Context, facts.Envelope) error

// Emit implements FactSink.
func (f FactSinkFunc) Emit(ctx context.Context, envelope facts.Envelope) error {
	if f == nil {
		return fmt.Errorf("terraform state fact sink func must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return f(ctx, envelope)
}

// ParseResult is the redacted fact output from one Terraform state parse.
type ParseResult struct {
	Facts             []facts.Envelope
	ResourceFacts     int64
	OutputFacts       int64
	ModuleFacts       int64
	WarningsByKind    map[string]int64
	RedactionsApplied map[string]int64
}

// ParseStreamResult is the bounded operational summary from one streaming
// Terraform state parse. WarningsByKind groups emitted warning_fact counts by
// warning_kind so callers can record one telemetry counter per kind without
// rescanning the streamed facts.
type ParseStreamResult struct {
	ResourceFacts     int64
	OutputFacts       int64
	ModuleFacts       int64
	WarningsByKind    map[string]int64
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
