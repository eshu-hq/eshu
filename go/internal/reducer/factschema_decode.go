// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
	iamv1 "github.com/eshu-hq/eshu/sdk/go/factschema/iam/v1"
)

// factDecodeError wraps a classified *factschema.DecodeError so the reducer's
// durable-queue failure path treats a malformed payload as a terminal,
// operator-facing dead letter rather than a retry or a silent zero value.
//
// It self-classifies through the same interface the Postgres queue reads
// (queueFailureMetadata via errors.As): FailureClass returns the DecodeError's
// classification string — "input_invalid" for a missing/null required field —
// which is byte-equal to projector.TriageClassInputInvalid and
// factschema.ClassificationInputInvalid by the by-value contract Contract System
// v1 mandates (the contracts module cannot import go/internal, so the reducer
// maps the classification by value). Retryable returns false because a missing
// required field can never succeed on replay unchanged; the intent must
// dead-letter, not loop.
type factDecodeError struct {
	// factKind is the fact kind that failed to decode, for the error message.
	factKind string
	// err is the underlying classified *factschema.DecodeError.
	err *factschema.DecodeError
}

// Error implements the error interface, naming the fact kind and the underlying
// classified decode failure.
func (e *factDecodeError) Error() string {
	return fmt.Sprintf("decode %s payload: %s", e.factKind, e.err.Error())
}

// Unwrap exposes the underlying *factschema.DecodeError so errors.As can reach
// it (and its ErrUnsupportedSchemaMajor sentinel).
func (e *factDecodeError) Unwrap() error {
	return e.err
}

// Retryable reports that a decode failure is terminal: replaying a fact with a
// missing or malformed required field can never succeed, so it must not re-enter
// the durable queue.
func (e *factDecodeError) Retryable() bool {
	return false
}

// FailureClass returns the durable failure_class the dead-letter row carries.
// It is the DecodeError's own classification value ("input_invalid"), so the
// reducer maps the contracts-module classification onto the queue's triage class
// by value without importing go/internal/projector.
func (e *factDecodeError) FailureClass() string {
	return e.err.Classification
}

// newFactDecodeError wraps a decode error returned by a factschema Decode*
// function into the reducer's self-classifying terminal failure. It expects a
// *factschema.DecodeError (the only error the Decode* seam returns); if a caller
// ever passes a different error it is wrapped with the input_invalid
// classification so the fact still dead-letters rather than being mistaken for a
// retryable projection bug.
func newFactDecodeError(factKind string, err error) *factDecodeError {
	var decodeErr *factschema.DecodeError
	if errors.As(err, &decodeErr) {
		return &factDecodeError{factKind: factKind, err: decodeErr}
	}
	return &factDecodeError{
		factKind: factKind,
		err: &factschema.DecodeError{
			FactKind:       factKind,
			Classification: factschema.ClassificationInputInvalid,
			Err:            err,
		},
	}
}

// quarantinedFact records one fact a batch extractor could not decode because
// its payload was missing a required field (an input_invalid decode failure).
// The extractor skips it (still projecting every valid fact in the batch) and
// returns it so the handler can emit a visible, per-fact dead-letter — a metric
// increment plus a structured error log naming the fact and field — rather than
// failing the whole intent or silently dropping the fact.
type quarantinedFact struct {
	// factID is the durable fact identifier of the malformed fact, so an
	// operator can locate the exact fact in fact_records.
	factID string
	// factKind is the malformed fact's kind.
	factKind string
	// field is the required payload key that was absent or null.
	field string
	// classification is the decode classification (always input_invalid for a
	// quarantined fact; a non-input_invalid error is never quarantined — it is
	// returned fatally by partitionDecodeFailures).
	classification string
}

// partitionDecodeFailures is the single classifier every batch extractor routes
// a decode error through. It enforces the reducer fault-isolation contract:
//
//   - A *factDecodeError with ClassificationInputInvalid (a missing/null
//     required field) is QUARANTINABLE: it returns a quarantinedFact and true,
//     so the extractor skips that one fact and keeps projecting the rest. The
//     fact is non-retryable — replaying it unchanged can never succeed — so
//     dropping it from the batch and recording it as a visible dead-letter is
//     correct, not a silent swallow.
//   - ANY OTHER error (a transient fact-load EOF, a graph-write failure, an
//     unsupported schema major, a projection bug) is FATAL: it returns
//     (zero, false, err), so the extractor propagates it and the handler fails
//     the whole intent through WorkSink.Fail, which triages it correctly
//     (retry_exhausted / dependency_unavailable / projection_bug / …).
//
// Routing every decode error through this ONE helper is what stops a future
// family-migration copy from inline `if err != nil { skip }` swallowing a
// transient loader or graph error — the "swallow failures" sin the Life Motto
// forbids. TestPartitionDecodeFailures locks the boundary.
func partitionDecodeFailures(env facts.Envelope, err error) (quarantinedFact, bool, error) {
	var decodeErr *factDecodeError
	// An unsupported schema major is version skew, not a malformed individual
	// payload: the contracts module currently labels it input_invalid, but it
	// must fail the whole work item for durable triage (it can succeed once the
	// reducer supports the major), never be quarantined and skipped per-fact.
	// Excluding the sentinel keeps this function matching its documented contract
	// above, where an unsupported schema major is listed as fatal.
	if errors.As(err, &decodeErr) &&
		decodeErr.err.Classification == factschema.ClassificationInputInvalid &&
		!errors.Is(err, factschema.ErrUnsupportedSchemaMajor) {
		return quarantinedFact{
			factID:         env.FactID,
			factKind:       env.FactKind,
			field:          decodeErr.err.Field,
			classification: decodeErr.err.Classification,
		}, true, nil
	}
	return quarantinedFact{}, false, err
}

// quarantinedAttributeShapeFact builds a quarantinedFact for a service-specific
// attribute field that failed one of the bounded typed-attribute Decode*
// functions in sdk/go/factschema/aws/v1 (issue #4631) — for example
// awsv1.DecodeResourceEC2VolumeAttributes rejecting a present-but-non-bool
// "encrypted" value. This is distinct from partitionDecodeFailures, which
// classifies a whole-envelope *factDecodeError: an attribute-shape failure
// happens AFTER the envelope's identity fields already decoded successfully,
// so the envelope itself is not malformed, only one service-specific field
// inside it. Routing it through the same quarantinedFact dead-letter surface
// keeps the failure visible (counted + logged) instead of silently
// substituting a zero/empty derived value, matching the accuracy contract a
// missing required field already gets.
//
// err's message (via *awsv1.AttributeShapeError.Error(), or any other error's
// Error() as a fallback) becomes the quarantine's field name so the field text
// is preserved even if a future caller passes a wrapped or non-AttributeShapeError
// value.
func quarantinedAttributeShapeFact(env facts.Envelope, err error) quarantinedFact {
	field := err.Error()
	var shapeErr *awsv1.AttributeShapeError
	if errors.As(err, &shapeErr) {
		field = shapeErr.Field
	}
	return quarantinedFact{
		factID:         env.FactID,
		factKind:       env.FactKind,
		field:          field,
		classification: factschema.ClassificationInputInvalid,
	}
}

// attributeShapeAsFactDecodeError adapts a service-specific attribute decode
// error (an *awsv1.AttributeShapeError from one of the bounded typed-attribute
// Decode* functions, issue #4631) into a *factDecodeError so a caller that
// already routes its envelope decode errors through partitionDecodeFailures
// (for example cloudResourceNodeRow) can propagate an attribute-shape failure
// through that same, unmodified quarantine path. The resulting
// *factDecodeError classifies as input_invalid and names the specific
// attribute field (not just the envelope) in its Field, so the quarantine's
// operator-facing "missing_field" log carries the precise failing path.
func attributeShapeAsFactDecodeError(factKind string, err error) error {
	field := err.Error()
	var shapeErr *awsv1.AttributeShapeError
	if errors.As(err, &shapeErr) {
		field = shapeErr.Field
	}
	return &factDecodeError{
		factKind: factKind,
		err: &factschema.DecodeError{
			FactKind:       factKind,
			Classification: factschema.ClassificationInputInvalid,
			Field:          field,
			Err:            err,
		},
	}
}

// recordQuarantinedFacts emits the visible, operator-diagnosable dead-letter for
// each fact a batch extractor quarantined during decode: it increments the
// eshu_dp_reducer_input_invalid_facts_total counter (labeled by domain and
// fact_kind) and logs one structured error per fact naming the fact id and the
// missing required field, then returns the count for the handler to record in
// Result.SubSignals["input_invalid_facts"]. This is the difference from the old
// silent skip — a quarantined fact is a first-class, dashboard-visible,
// log-searchable event, not an anonymous counter bump.
//
// It is safe to call with a nil instruments pointer (the counter is skipped, the
// logs still emit) and with an empty slice (a no-op returning 0).
//
// It also persists the quarantined facts to the durable
// reducer_input_invalid_facts read surface (issue #4630) through the writer
// stashed on ctx by Service.executeWithTelemetry (see WithQuarantineWriter in
// quarantine_writer.go), via persistQuarantinedFacts. That persistence is
// strictly best-effort: a write failure is logged and counted but NEVER
// returned, so a durable-write outage can never turn a per-fact quarantine
// (which is by design non-fatal) into a fatal intent failure.
func recordQuarantinedFacts(
	ctx context.Context,
	instruments *telemetry.Instruments,
	domain Domain,
	scopeID, generationID string,
	quarantined []quarantinedFact,
) int {
	if len(quarantined) == 0 {
		return 0
	}
	now := time.Now().UTC()
	records := make([]QuarantinedFactRecord, 0, len(quarantined))
	for _, q := range quarantined {
		if instruments != nil && instruments.ReducerInputInvalidFacts != nil {
			instruments.ReducerInputInvalidFacts.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrDomain(string(domain)),
				telemetry.AttrFactKind(q.factKind),
			))
		}
		slog.ErrorContext(
			ctx, "reducer input_invalid fact quarantined",
			log.Domain(string(domain)),
			log.ScopeID(scopeID),
			log.GenerationID(generationID),
			slog.String("fact_id", q.factID),
			slog.String("fact_kind", q.factKind),
			slog.String("missing_field", q.field),
			slog.String("failure_class", q.classification),
		)
		records = append(records, QuarantinedFactRecord{
			FactID:       q.factID,
			FactKind:     q.factKind,
			MissingField: q.field,
			FailureClass: q.classification,
			Domain:       string(domain),
			ScopeID:      scopeID,
			GenerationID: generationID,
			DecidedAt:    now,
		})
	}
	persistQuarantinedFacts(ctx, quarantineWriterFromContext(ctx), instruments, records)
	return len(quarantined)
}

// persistQuarantinedFacts writes records to writer in one batched round trip
// (per intent, not per fact) and records batch-size/committed/error telemetry.
// A nil writer (the default: Service.QuarantineWriter unset, or every
// handler-level unit test) or an empty records slice makes this a no-op. A
// write error is logged and counted through
// eshu_dp_reducer_input_invalid_fact_write_errors_total (reason=write_error)
// and NEVER returned: this durable row is an operator convenience read
// surface, not a correctness dependency, so an outage in it must never
// dead-letter or fail the owning intent (which already correctly quarantined
// the fact via the counter/log above regardless of this write's outcome).
func persistQuarantinedFacts(
	ctx context.Context,
	writer QuarantinedFactWriter,
	instruments *telemetry.Instruments,
	records []QuarantinedFactRecord,
) {
	if writer == nil || len(records) == 0 {
		return
	}
	if instruments != nil && instruments.ReducerInputInvalidFactWriteBatchSize != nil {
		instruments.ReducerInputInvalidFactWriteBatchSize.Record(ctx, float64(len(records)))
	}
	if err := writer.WriteQuarantinedFacts(ctx, records); err != nil {
		if instruments != nil && instruments.ReducerInputInvalidFactWriteErrors != nil {
			instruments.ReducerInputInvalidFactWriteErrors.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrReason("write_error"),
			))
		}
		slog.ErrorContext(
			ctx, "reducer input_invalid fact quarantine durable write failed; continuing without durable row (best-effort, non-fatal)",
			slog.Int("record_count", len(records)),
			slog.String("error", err.Error()),
		)
		return
	}
	if instruments != nil && instruments.ReducerInputInvalidFactsCommitted != nil {
		instruments.ReducerInputInvalidFactsCommitted.Add(ctx, int64(len(records)))
	}
}

// inputInvalidSubSignals returns the Result.SubSignals map carrying the count of
// facts quarantined as input_invalid during this intent, or nil when none were.
// Returning nil for the zero case keeps the service log line from emitting a
// noise "input_invalid_facts=0" signal on the overwhelming majority of intents
// that decode cleanly; a non-zero count is the operator's per-intent flag that
// this intent skipped malformed facts (each one also on the counter + error log).
func inputInvalidSubSignals(count int) map[string]float64 {
	if count == 0 {
		return nil
	}
	return map[string]float64{"input_invalid_facts": float64(count)}
}

// decodeAWSResource decodes one aws_resource envelope into the typed
// awsv1.Resource struct through the contracts seam, returning a self-classifying
// *factDecodeError when the payload is missing a required field or otherwise
// malformed. It is the single decode site for the aws_resource kind on the
// reducer side: every handler and join-index builder that consumes aws_resource
// facts decodes through here, and a missing required field is routed through
// partitionDecodeFailures so it dead-letters as a per-fact input_invalid
// quarantine rather than a silent empty-string graph identity or a whole-intent
// abort.
func decodeAWSResource(env facts.Envelope) (awsv1.Resource, error) {
	resource, err := factschema.DecodeAWSResource(factschemaEnvelope(env))
	if err != nil {
		return awsv1.Resource{}, newFactDecodeError(factschema.FactKindAWSResource, err)
	}
	return resource, nil
}

// decodeAWSRelationship decodes one aws_relationship envelope into the typed
// awsv1.Relationship struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required field
// (account_id, region, relationship_type, source_resource_id,
// target_resource_id) or is otherwise malformed. It is the single decode site
// for the aws_relationship kind on the reducer side.
func decodeAWSRelationship(env facts.Envelope) (awsv1.Relationship, error) {
	relationship, err := factschema.DecodeAWSRelationship(factschemaEnvelope(env))
	if err != nil {
		return awsv1.Relationship{}, newFactDecodeError(factschema.FactKindAWSRelationship, err)
	}
	return relationship, nil
}

// decodeAWSSecurityGroupRule decodes one aws_security_group_rule envelope into
// the typed awsv1.SecurityGroupRule struct through the contracts seam, returning
// a self-classifying *factDecodeError when the payload is missing a required
// field (account_id, region, group_id, direction, ip_protocol, source_kind,
// source_value). It is the single decode site for this kind on the reducer side.
func decodeAWSSecurityGroupRule(env facts.Envelope) (awsv1.SecurityGroupRule, error) {
	rule, err := factschema.DecodeAWSSecurityGroupRule(factschemaEnvelope(env))
	if err != nil {
		return awsv1.SecurityGroupRule{}, newFactDecodeError(factschema.FactKindAWSSecurityGroupRule, err)
	}
	return rule, nil
}

// decodeEC2InstancePosture decodes one ec2_instance_posture envelope into the
// typed awsv1.EC2InstancePosture struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required field
// (account_id, region). It is the single decode site for this kind on the
// reducer side.
func decodeEC2InstancePosture(env facts.Envelope) (awsv1.EC2InstancePosture, error) {
	posture, err := factschema.DecodeEC2InstancePosture(factschemaEnvelope(env))
	if err != nil {
		return awsv1.EC2InstancePosture{}, newFactDecodeError(factschema.FactKindEC2InstancePosture, err)
	}
	return posture, nil
}

// decodeS3BucketPosture decodes one s3_bucket_posture envelope into the typed
// awsv1.S3BucketPosture struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required field
// (account_id, region). It is the single decode site for this kind on the
// reducer side.
func decodeS3BucketPosture(env facts.Envelope) (awsv1.S3BucketPosture, error) {
	posture, err := factschema.DecodeS3BucketPosture(factschemaEnvelope(env))
	if err != nil {
		return awsv1.S3BucketPosture{}, newFactDecodeError(factschema.FactKindS3BucketPosture, err)
	}
	return posture, nil
}

// decodeRDSInstancePosture decodes one rds_instance_posture envelope into the
// typed awsv1.RDSInstancePosture struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// field (account_id, region, publicly_accessible, storage_encrypted,
// iam_database_authentication_enabled, multi_az, deletion_protection,
// backup_retention_period, performance_insights_enabled,
// performance_insights_retention_days — every non-pointer field the collector's
// NewRDSInstancePostureEnvelope always stamps). It is the single decode site
// for this kind on the reducer side: ExtractRDSPostureRows decodes through
// here so a fact missing its account/region identity dead-letters as a
// per-fact input_invalid quarantine instead of fabricating a
// CloudResource uid from an empty account_id/region.
func decodeRDSInstancePosture(env facts.Envelope) (awsv1.RDSInstancePosture, error) {
	posture, err := factschema.DecodeRDSInstancePosture(factschemaEnvelope(env))
	if err != nil {
		return awsv1.RDSInstancePosture{}, newFactDecodeError(factschema.FactKindRDSInstancePosture, err)
	}
	return posture, nil
}

// decodeS3ExternalPrincipalGrant decodes one s3_external_principal_grant
// envelope into the typed awsv1.S3ExternalPrincipalGrant struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing a required field (account_id, region, principal_kind,
// principal_value, grant_outcome, is_public, is_cross_account,
// is_service_principal, is_unsupported — every non-pointer field the
// collector's NewS3ExternalPrincipalGrantEnvelope always stamps). It is the
// single decode site for this kind on the reducer side:
// ExtractS3ExternalPrincipalGrantRows decodes through here so a fact missing
// its account/region or principal identity dead-letters as a per-fact
// input_invalid quarantine instead of fabricating a GRANTS_ACCESS_TO edge from
// an empty principal identity.
func decodeS3ExternalPrincipalGrant(env facts.Envelope) (awsv1.S3ExternalPrincipalGrant, error) {
	grant, err := factschema.DecodeS3ExternalPrincipalGrant(factschemaEnvelope(env))
	if err != nil {
		return awsv1.S3ExternalPrincipalGrant{}, newFactDecodeError(factschema.FactKindS3ExternalPrincipalGrant, err)
	}
	return grant, nil
}

// decodeAWSIAMPermission decodes one aws_iam_permission envelope into the typed
// iamv1.Permission struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required field
// (account_id, region, principal_arn, effect, policy_source). It is the single
// decode site for this kind on the reducer side.
func decodeAWSIAMPermission(env facts.Envelope) (iamv1.Permission, error) {
	permission, err := factschema.DecodeAWSIAMPermission(factschemaEnvelope(env))
	if err != nil {
		return iamv1.Permission{}, newFactDecodeError(factschema.FactKindAWSIAMPermission, err)
	}
	return permission, nil
}

// decodeAWSResourcePolicyPermission decodes one aws_resource_policy_permission
// envelope into the typed iamv1.ResourcePolicyPermission struct through the
// contracts seam, returning a self-classifying *factDecodeError when the payload
// is missing a required field (account_id, region, resource_arn, resource_type,
// effect). It is the single decode site for this kind on the reducer side.
func decodeAWSResourcePolicyPermission(env facts.Envelope) (iamv1.ResourcePolicyPermission, error) {
	permission, err := factschema.DecodeAWSResourcePolicyPermission(factschemaEnvelope(env))
	if err != nil {
		return iamv1.ResourcePolicyPermission{}, newFactDecodeError(factschema.FactKindAWSResourcePolicyPermission, err)
	}
	return permission, nil
}

// decodeAWSIAMPrincipal decodes one aws_iam_principal envelope into the typed
// iamv1.Principal struct through the contracts seam, returning a self-classifying
// *factDecodeError when the payload is missing a required field (account_id,
// region, principal_arn, principal_type). It is the single decode site for this
// kind on the reducer side.
func decodeAWSIAMPrincipal(env facts.Envelope) (iamv1.Principal, error) {
	principal, err := factschema.DecodeAWSIAMPrincipal(factschemaEnvelope(env))
	if err != nil {
		return iamv1.Principal{}, newFactDecodeError(factschema.FactKindAWSIAMPrincipal, err)
	}
	return principal, nil
}

// decodeGCPCloudResource decodes one gcp_cloud_resource envelope into the typed
// gcpv1.Resource struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// field (full_resource_name, asset_type) or is otherwise malformed. It is the
// single decode site for the gcp_cloud_resource kind on the reducer side:
// every handler and join-index builder that consumes gcp_cloud_resource facts
// decodes through here, and a missing required field is routed through
// partitionDecodeFailures so it dead-letters as a per-fact input_invalid
// quarantine rather than a silent empty-string graph identity or a whole-intent
// abort. This mirrors decodeAWSResource.
func decodeGCPCloudResource(env facts.Envelope) (gcpv1.Resource, error) {
	resource, err := factschema.DecodeGCPCloudResource(factschemaEnvelope(env))
	if err != nil {
		return gcpv1.Resource{}, newFactDecodeError(factschema.FactKindGCPCloudResource, err)
	}
	return resource, nil
}

// decodeGCPCloudRelationship decodes one gcp_cloud_relationship envelope into
// the typed gcpv1.Relationship struct through the contracts seam, returning a
// self-classifying *factDecodeError when the payload is missing a required
// field (source_full_resource_name, target_full_resource_name,
// relationship_type) or is otherwise malformed. It is the single decode site
// for the gcp_cloud_relationship kind on the reducer side. This mirrors
// decodeAWSRelationship.
func decodeGCPCloudRelationship(env facts.Envelope) (gcpv1.Relationship, error) {
	relationship, err := factschema.DecodeGCPCloudRelationship(factschemaEnvelope(env))
	if err != nil {
		return gcpv1.Relationship{}, newFactDecodeError(factschema.FactKindGCPCloudRelationship, err)
	}
	return relationship, nil
}

// factschemaEnvelope adapts a go/internal/facts.Envelope to the contracts-module
// factschema.Envelope the Decode* seam accepts through the generated shared
// adapter. Keeping this wrapper preserves the reducer-local call sites while
// making factenvelope the single source for field mapping and version-less
// schema normalization.
//
// A version-less SchemaVersion is normalized to the current major-1 schema
// version. "Version-less" means either an empty string (what a fact carries
// in-memory before persistence) OR the sentinel "0.0.0" that the Postgres
// persist layer stamps for a fact its collector emitted with no version
// (go/internal/storage/postgres/facts.go, facts_streaming.go:
// emptyToDefault(SchemaVersion, "0.0.0")). A fact LOADED from Postgres for
// reduction therefore carries "0.0.0", not "", so both spellings of
// "the collector emitted no version" must normalize identically — otherwise a
// version-less family loaded from storage (the git code family: "file",
// "repository") dead-letters as an unsupported major and its whole graph
// collapses (PR #4753 corpus-gate P0). "0.0.0" is used nowhere as a real
// schema version — it is exclusively the persist-layer's empty marker
// (schemaVersionPattern accepts it, but no collector emits it), so treating it
// as version-less is safe for every other family, all of which stamp a concrete
// "1.0.0".
//
// This does NOT weaken accuracy: a present, genuine, unsupported major (for
// example "2.0.0") is NOT normalized and still dead-letters through the Decode*
// seam's default branch, and a fact missing a required identity field still
// dead-letters as input_invalid regardless of its version.
func factschemaEnvelope(env facts.Envelope) factschema.Envelope {
	return factenvelope.FactSchemaFromInternal(env)
}
