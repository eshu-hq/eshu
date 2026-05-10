// Package tfstateruntime adapts Terraform-state reader primitives to the
// workflow-claimed collector runtime.
package tfstateruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// SourceFactory opens an exact Terraform state source for a resolved candidate.
type SourceFactory interface {
	OpenSource(context.Context, terraformstate.DiscoveryCandidate) (terraformstate.StateSource, error)
}

// SourceFactoryFunc adapts a function into a SourceFactory.
type SourceFactoryFunc func(context.Context, terraformstate.DiscoveryCandidate) (terraformstate.StateSource, error)

// OpenSource implements SourceFactory.
func (f SourceFactoryFunc) OpenSource(
	ctx context.Context,
	candidate terraformstate.DiscoveryCandidate,
) (terraformstate.StateSource, error) {
	return f(ctx, candidate)
}

// DefaultSourceFactory opens the built-in local and S3 Terraform state source
// types. S3 still depends on a caller-supplied read-only object client.
type DefaultSourceFactory struct {
	S3Client                terraformstate.S3ObjectClient
	S3FallbackLockTableName string
	S3LockMetadataClient    terraformstate.LockMetadataClient
	MaxBytes                int64
}

// OpenSource implements SourceFactory.
func (f DefaultSourceFactory) OpenSource(
	_ context.Context,
	candidate terraformstate.DiscoveryCandidate,
) (terraformstate.StateSource, error) {
	if err := candidate.Validate(); err != nil {
		return nil, err
	}
	switch candidate.State.BackendKind {
	case terraformstate.BackendLocal:
		stateSource, err := terraformstate.NewLocalStateSource(terraformstate.LocalSourceConfig{
			Path:     candidate.State.Locator,
			MaxBytes: f.MaxBytes,
		})
		if err != nil {
			return nil, sourceFailure("build", candidate.State, err)
		}
		return stateSource, nil
	case terraformstate.BackendS3:
		bucket, key, err := parseS3Locator(candidate.State.Locator)
		if err != nil {
			return nil, err
		}
		lockTableName, lockID, lockClient := f.s3LockConfig(candidate, bucket, key)
		stateSource, err := terraformstate.NewS3StateSource(terraformstate.S3SourceConfig{
			Bucket:        bucket,
			Key:           key,
			Region:        candidate.Region,
			VersionID:     candidate.State.VersionID,
			PreviousETag:  candidate.PreviousETag,
			MaxBytes:      f.MaxBytes,
			Client:        f.S3Client,
			LockTableName: lockTableName,
			LockID:        lockID,
			LockClient:    lockClient,
		})
		if err != nil {
			return nil, sourceFailure("build", candidate.State, err)
		}
		return stateSource, nil
	default:
		return nil, fmt.Errorf("unsupported terraform state backend kind %q", candidate.State.BackendKind)
	}
}

func (f DefaultSourceFactory) s3LockConfig(
	candidate terraformstate.DiscoveryCandidate,
	bucket string,
	key string,
) (string, string, terraformstate.LockMetadataClient) {
	tableName := strings.TrimSpace(candidate.DynamoDBTable)
	if tableName == "" {
		tableName = strings.TrimSpace(f.S3FallbackLockTableName)
	}
	if tableName == "" {
		return "", "", nil
	}
	return tableName, bucket + "/" + key + "-md5", f.S3LockMetadataClient
}

// ClaimedSource resolves exact Terraform-state candidates and returns the one
// generation that matches the already-claimed workflow item.
type ClaimedSource struct {
	Resolver       terraformstate.DiscoveryResolver
	SourceFactory  SourceFactory
	RedactionKey   redact.Key
	RedactionRules redact.RuleSet
	Clock          func() time.Time
	Tracer         trace.Tracer
	Instruments    *telemetry.Instruments
}

// NextClaimed implements collector.ClaimedSource for Terraform state work.
func (s ClaimedSource) NextClaimed(
	ctx context.Context,
	item workflow.WorkItem,
) (collector.CollectedGeneration, bool, error) {
	if err := s.validate(item); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return collector.CollectedGeneration{}, false, err
	}

	candidates, err := s.Resolver.Resolve(ctx)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	for _, candidate := range candidates {
		collected, ok, err := s.collectCandidate(ctx, item, candidate)
		if err != nil {
			return collector.CollectedGeneration{}, false, err
		}
		if ok {
			return collected, true, nil
		}
	}
	return collector.CollectedGeneration{}, false, nil
}

func (s ClaimedSource) validate(item workflow.WorkItem) error {
	if s.SourceFactory == nil {
		return fmt.Errorf("terraform state source factory is required")
	}
	if s.RedactionKey.IsZero() {
		return fmt.Errorf("terraform state redaction key is required")
	}
	if item.CollectorKind != scope.CollectorTerraformState {
		return fmt.Errorf("claimed collector_kind %q must be %q", item.CollectorKind, scope.CollectorTerraformState)
	}
	if strings.TrimSpace(item.SourceSystem) != string(scope.CollectorTerraformState) {
		return fmt.Errorf("claimed source_system %q must be %q", item.SourceSystem, scope.CollectorTerraformState)
	}
	if item.CurrentFencingToken <= 0 {
		return fmt.Errorf("claimed terraform state fencing token must be positive")
	}
	return nil
}

func (s ClaimedSource) collectCandidate(
	ctx context.Context,
	item workflow.WorkItem,
	candidate terraformstate.DiscoveryCandidate,
) (collector.CollectedGeneration, bool, error) {
	if err := candidate.Validate(); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	candidateScope, err := scopeForCandidate(candidate, candidate.State)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	candidateID, err := terraformstate.CandidatePlanningID(candidate)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if !claimMatchesCandidate(item, candidateScope, candidateID) {
		return collector.CollectedGeneration{}, false, nil
	}
	stateSource, err := s.SourceFactory.OpenSource(ctx, candidate)
	if err != nil {
		s.recordSnapshotObserved(ctx, candidate.State.BackendKind, "error")
		return collector.CollectedGeneration{}, false, sourceFailure("build", candidate.State, err)
	}
	if stateSource == nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("terraform state source factory returned nil source")
	}
	sourceKey := stateSource.Identity()
	if err := sourceKey.Validate(); err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if err := ensureSourceIdentity(candidate.State, sourceKey); err != nil {
		return collector.CollectedGeneration{}, false, err
	}

	identity, observedAt, err := s.readIdentity(ctx, stateSource)
	if err != nil {
		if errors.Is(err, terraformstate.ErrStateNotModified) {
			s.recordS3NotModified(ctx, candidate.State.BackendKind)
			s.recordSnapshotObserved(ctx, candidate.State.BackendKind, "not_modified")
			if strings.TrimSpace(candidate.PriorGenerationID) == "" && usesCandidatePlanningID(item) {
				return collector.CollectedGeneration{}, false, nil
			}
			return collector.CollectedGeneration{Unchanged: true}, true, nil
		}
		if errors.Is(err, terraformstate.ErrStateTooLarge) {
			s.recordSnapshotObserved(ctx, candidate.State.BackendKind, "state_too_large")
			collected, warningErr := s.stateTooLargeWarningGeneration(
				candidate,
				candidateScope,
				candidateID,
				sourceKey,
				item.CurrentFencingToken,
			)
			if warningErr != nil {
				return collector.CollectedGeneration{}, false, warningErr
			}
			return collected, true, nil
		}
		s.recordSnapshotObserved(ctx, candidate.State.BackendKind, "error")
		return collector.CollectedGeneration{}, false, err
	}
	scopeValue, generationValue, err := generationForCandidate(candidate, sourceKey, identity, observedAt)
	if err != nil {
		return collector.CollectedGeneration{}, false, err
	}
	if !claimMatchesCollected(item, scopeValue, generationValue, candidateID) {
		return collector.CollectedGeneration{}, false, nil
	}

	result, _, err := s.parseCandidate(ctx, stateSource, candidate, scopeValue, generationValue, sourceKey, item.CurrentFencingToken)
	if err != nil {
		s.recordSnapshotObserved(ctx, sourceKey.BackendKind, "error")
		return collector.CollectedGeneration{}, false, err
	}
	s.recordSnapshotObserved(ctx, sourceKey.BackendKind, "parsed")
	s.recordResourceFacts(ctx, sourceKey.BackendKind, result.ResourceFacts)
	s.recordRedactions(ctx, result.RedactionsApplied)
	return collector.FactsFromSlice(scopeValue, generationValue, result.Facts), true, nil
}

func (s ClaimedSource) readIdentity(
	ctx context.Context,
	stateSource terraformstate.StateSource,
) (terraformstate.SnapshotIdentity, time.Time, error) {
	reader, metadata, err := s.openSource(ctx, stateSource)
	if err != nil {
		return terraformstate.SnapshotIdentity{}, time.Time{}, err
	}
	defer closeReader(reader)

	identity, err := terraformstate.ReadSnapshotIdentity(ctx, reader)
	if err != nil {
		return terraformstate.SnapshotIdentity{}, time.Time{}, fmt.Errorf("read terraform state identity: %w", err)
	}
	return identity, firstTime(metadata.ObservedAt, s.now()), nil
}

func (s ClaimedSource) parseCandidate(
	ctx context.Context,
	stateSource terraformstate.StateSource,
	candidate terraformstate.DiscoveryCandidate,
	scopeValue scope.IngestionScope,
	generationValue scope.ScopeGeneration,
	sourceKey terraformstate.StateKey,
	fencingToken int64,
) (terraformstate.ParseResult, terraformstate.SourceMetadata, error) {
	reader, metadata, err := s.openSource(ctx, stateSource)
	if err != nil {
		return terraformstate.ParseResult{}, terraformstate.SourceMetadata{}, err
	}
	defer closeReader(reader)

	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanTerraformStateParserStream)
		defer span.End()
	}
	start := time.Now()
	result, err := terraformstate.Parse(ctx, reader, terraformstate.ParseOptions{
		Scope:          scopeValue,
		Generation:     generationValue,
		Source:         sourceKey,
		Metadata:       metadata,
		ObservedAt:     generationValue.ObservedAt,
		RedactionKey:   s.RedactionKey,
		RedactionRules: s.RedactionRules,
		FencingToken:   fencingToken,
		SourceWarnings: sourceWarningsForCandidate(candidate),
	})
	s.recordParseDuration(ctx, sourceKey.BackendKind, time.Since(start))
	if err != nil {
		return terraformstate.ParseResult{}, terraformstate.SourceMetadata{}, fmt.Errorf("parse terraform state: %w", err)
	}
	s.recordSnapshotBytes(ctx, sourceKey.BackendKind, metadata.Size)
	return result, metadata, nil
}

func sourceWarningsForCandidate(candidate terraformstate.DiscoveryCandidate) []terraformstate.SourceWarning {
	if !candidate.StateInVCS {
		return nil
	}
	return []terraformstate.SourceWarning{{
		WarningKind: "state_in_vcs",
		Reason:      "terraform state file was discovered in git and explicitly approved for ingestion",
		Source:      string(candidate.Source),
	}}
}

func (s ClaimedSource) openSource(
	ctx context.Context,
	stateSource terraformstate.StateSource,
) (io.ReadCloser, terraformstate.SourceMetadata, error) {
	if s.Tracer != nil {
		var span trace.Span
		ctx, span = s.Tracer.Start(ctx, telemetry.SpanTerraformStateSourceOpen)
		defer span.End()
	}
	reader, metadata, err := stateSource.Open(ctx)
	if err != nil {
		return nil, terraformstate.SourceMetadata{}, sourceFailure("open", stateSource.Identity(), err)
	}
	return reader, metadata, nil
}

func generationForCandidate(
	candidate terraformstate.DiscoveryCandidate,
	sourceKey terraformstate.StateKey,
	identity terraformstate.SnapshotIdentity,
	observedAt time.Time,
) (scope.IngestionScope, scope.ScopeGeneration, error) {
	scopeValue, err := scopeForCandidate(candidate, sourceKey)
	if err != nil {
		return scope.IngestionScope{}, scope.ScopeGeneration{}, err
	}
	generationValue, err := scope.NewTerraformStateSnapshotGeneration(
		scopeValue.ScopeID,
		identity.Serial,
		identity.Lineage,
		observedAt,
	)
	if err != nil {
		return scope.IngestionScope{}, scope.ScopeGeneration{}, err
	}
	return scopeValue, generationValue, nil
}

func scopeForCandidate(
	candidate terraformstate.DiscoveryCandidate,
	sourceKey terraformstate.StateKey,
) (scope.IngestionScope, error) {
	metadata := map[string]string{}
	if repoID := strings.TrimSpace(candidate.RepoID); repoID != "" {
		metadata["repo_id"] = repoID
	}
	return scope.NewTerraformStateSnapshotScope(
		strings.TrimSpace(candidate.RepoID),
		string(sourceKey.BackendKind),
		sourceKey.Locator,
		metadata,
	)
}

func ensureSourceIdentity(expected terraformstate.StateKey, actual terraformstate.StateKey) error {
	if expected == actual {
		return nil
	}
	return fmt.Errorf("terraform state source identity mismatch for %s candidate", expected.BackendKind)
}

func claimMatchesCandidate(item workflow.WorkItem, scopeValue scope.IngestionScope, candidateID string) bool {
	if item.ScopeID != scopeValue.ScopeID {
		return false
	}
	if !usesCandidatePlanningID(item) {
		return true
	}
	return item.GenerationID == candidateID && item.SourceRunID == candidateID
}

func claimMatchesCollected(
	item workflow.WorkItem,
	scopeValue scope.IngestionScope,
	generationValue scope.ScopeGeneration,
	candidateID string,
) bool {
	if item.ScopeID != scopeValue.ScopeID {
		return false
	}
	if usesCandidatePlanningID(item) {
		return item.GenerationID == candidateID && item.SourceRunID == candidateID
	}
	return item.GenerationID == generationValue.GenerationID && item.SourceRunID == generationValue.GenerationID
}

func usesCandidatePlanningID(item workflow.WorkItem) bool {
	return terraformstate.IsCandidatePlanningID(item.GenerationID) ||
		terraformstate.IsCandidatePlanningID(item.SourceRunID)
}

func (s ClaimedSource) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func firstTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Now().UTC()
}

func closeReader(reader io.Closer) {
	if reader != nil {
		_ = reader.Close()
	}
}

func parseS3Locator(locator string) (string, string, error) {
	rest, ok := strings.CutPrefix(locator, "s3://")
	if !ok {
		return "", "", fmt.Errorf("s3 state locator must start with s3://")
	}
	bucket, key, ok := strings.Cut(rest, "/")
	if !ok || strings.TrimSpace(bucket) == "" || strings.TrimSpace(key) == "" {
		return "", "", fmt.Errorf("s3 state locator must include bucket and key")
	}
	return bucket, key, nil
}

func sourceFailure(action string, state terraformstate.StateKey, err error) error {
	var existing sourceError
	if errors.As(err, &existing) {
		return err
	}
	message := err.Error()
	if locator := strings.TrimSpace(state.Locator); locator != "" {
		message = strings.ReplaceAll(message, locator, "<redacted>")
	}
	return sourceError{
		action:      action,
		backendKind: state.BackendKind,
		message:     message,
		cause:       err,
	}
}

type sourceError struct {
	action      string
	backendKind terraformstate.BackendKind
	message     string
	cause       error
}

func (e sourceError) Error() string {
	return fmt.Sprintf("%s terraform state %s source: %s", e.action, e.backendKind, e.message)
}

func (e sourceError) Unwrap() error {
	return e.cause
}
