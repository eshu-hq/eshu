// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/projector/decode"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorintent "github.com/eshu-hq/eshu/go/internal/projector/intent"
	projectorentity "github.com/eshu-hq/eshu/go/internal/projector/semantic/entity"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// CanonicalWriter writes canonical Neo4j nodes from a CanonicalMaterialization.
type CanonicalWriter interface {
	Write(context.Context, canonical.CanonicalMaterialization) error
}

// Runtime is one projector's unit of work: the injected writers, the identity
// locker and the optional observability collaborators that Project needs to
// turn a generation's facts into canonical graph and content truth. It carries
// no per-run state, so one value is reusable across generations.
type Runtime struct {
	CanonicalWriter CanonicalWriter // replaces GraphWriter — canonical graph projection
	ContentWriter   content.Writer
	IntentWriter    ReducerIntentWriter
	PhasePublisher  gpphase.PhasePublisher
	RepairQueue     gpphase.PhaseRepairQueue
	RetryInjector   failure.RetryInjector
	// PackageRegistryIdentityLocker coordinates package UID writes across
	// projector processes before canonical graph projection reaches the backend.
	PackageRegistryIdentityLocker PackageRegistryIdentityLocker
	// ContentBeforeCanonical writes the content index before graph projection.
	// This is reserved for local profiles that must keep code search usable
	// while an evaluation graph backend is degraded.
	ContentBeforeCanonical bool
	Tracer                 trace.Tracer           // optional
	Instruments            *telemetry.Instruments // optional
	Logger                 *slog.Logger           // optional
}

// ReducerIntent is a compatibility alias for the dependency-neutral intent
// contract reserved for extracted projector family packages.
type ReducerIntent = projectorintent.ReducerIntent

// IntentResult reports the outcome of one Enqueue call. Count is the number
// of reducer intents actually admitted -- for postgres.ReducerQueue, the
// durable production ReducerIntentWriter, that is the underlying INSERT's
// RowsAffected, NOT len(intents) (issue #5593): the
// enqueue SQL's `ON CONFLICT (work_item_id) DO NOTHING` silently skips any
// intent whose work_item_id another producer already wrote, so an "attempted"
// count would over-report for every domain more than one producer can enqueue
// into, including config_state_drift (bootstrap Phase 3.5 and the runtime
// delta-trigger both target the same work_item_id per generation).
type IntentResult struct {
	Count int
}

// ReducerIntentWriter is the injected collaborator that admits reducer intents
// into the queue. It is an interface so the projector can be driven against a
// fake in tests, and production wires more than one implementation:
// postgres.ReducerQueue is the durable one, and the local_lightweight profile
// substitutes a writer that returns len(intents). IntentResult.Count therefore
// means "admitted" only for implementations that report what the write
// accepted -- see IntentResult. cmd/ingester's admission-aware decorator does
// not affect that: it defers or delegates, returning the inner writer's result
// unchanged.
type ReducerIntentWriter interface {
	// Enqueue admits intents into the reducer queue and reports how many rows
	// were actually inserted in the returned IntentResult.Count -- see
	// IntentResult's doc comment for why that is not always len(intents).
	Enqueue(context.Context, []ReducerIntent) (IntentResult, error)
}

// Result is what one projection reports back to the service loop: the scope and
// generation it covered, the content-index outcome, and the IntentResult its
// IntentWriter reported. Intents.Count carries whatever that writer reports --
// the admitted count for writers that report what the write accepted, or
// len(intents) under local_lightweight -- so read it with the writer in mind.
// See IntentResult and ReducerIntentWriter.
type Result struct {
	ScopeID      string
	GenerationID string
	Content      content.Result
	Intents      IntentResult
}

func (r Result) ScopeGenerationKey() string {
	return fmt.Sprintf("%s:%s", r.ScopeID, r.GenerationID)
}

func (Runtime) TraceSpanName() string {
	return telemetry.SpanProjectorRun
}

func (Runtime) TraceSpanNames() []string {
	return []string{
		telemetry.SpanProjectorRun,
		telemetry.SpanReducerIntentEnqueue,
		telemetry.SpanCanonicalWrite,
	}
}

func (r Runtime) Project(ctx context.Context, scopeValue scope.IngestionScope, generation scope.ScopeGeneration, inputFacts []facts.Envelope) (Result, error) {
	if err := generation.ValidateForScope(scopeValue); err != nil {
		return Result{}, err
	}

	// Ref-scoped repository scopes are gated from canonical projection. The
	// canonical graph must contain only the default branch; multi-ref overlays
	// are isolated to their own namespace (epic #5393, enabler #5417).
	// This is a fail-closed gate: return immediately with an empty result,
	// performing NO content writes and NO canonical graph writes.
	if scopeValue.ScopeKind == scope.KindRepositoryRef {
		if r.Logger != nil {
			r.Logger.InfoContext(ctx, "projector gated ref scope (non-default-branch facts excluded from canonical graph)",
				slog.String("scope_id", scopeValue.ScopeID),
				slog.String("ref", scopeValue.Metadata["ref"]),
				slog.String("reason", "ref_scope_gated"),
				slog.Int("fact_count", len(inputFacts)),
				slog.Int("file_fact_count", len(decode.FilterFileFacts(inputFacts))),
			)
		}
		return Result{
			ScopeID:      scopeValue.ScopeID,
			GenerationID: generation.GenerationID,
		}, nil
	}

	buildStart := time.Now()
	projection, err := buildProjection(scopeValue, generation, inputFacts)
	if err != nil {
		return Result{}, err
	}
	// Record any facts a typed canonical extractor quarantined during decode as
	// visible input_invalid dead-letters (a per-fact metric + structured error
	// log). The facts are already skipped from projection; this makes each one
	// operator-diagnosable rather than a silent drop. Recording never fails the
	// projection — a malformed fact must not stall the whole generation.
	//
	// projection.quarantinedFacts is the MERGED slice across every typed
	// canonical extractor (terraform_state, oci_registry, ...), so each fact is
	// grouped by its OWN originating stage (QuarantinedFactStage) before
	// recording — a single hardcoded stage label here would misattribute, for
	// example, a terraform_state quarantine to the oci_registry_canonical stage
	// in both the metric and the structured log an operator reads at 3am.
	for stage, group := range decode.GroupQuarantinedFactsByStage(projection.quarantinedFacts) {
		decode.RecordQuarantinedFacts(
			ctx, r.Instruments, stage,
			scopeValue.ScopeID, generation.GenerationID, group,
		)
	}
	if r.Instruments != nil {
		r.Instruments.ProjectorStageDuration.Record(ctx, time.Since(buildStart).Seconds(), metric.WithAttributes(
			telemetry.AttrScopeKind(string(scopeValue.ScopeKind)),
			attribute.String("stage", "build_projection"),
		))
	}
	r.logRuntimeStage(
		ctx, scopeValue, generation.GenerationID, "build_projection", buildStart,
		"fact_count", len(inputFacts),
		"generation_fact_count", projection.ownedFactCount,
		"union_base_fact_count", len(inputFacts)-projection.ownedFactCount,
		"content_record_count", len(projection.contentMaterialization.Records),
		"content_entity_count", len(projection.contentMaterialization.Entities),
		"content_repository_ref_count", len(projection.contentMaterialization.RepositoryRefs),
		"reducer_intent_count", len(projection.reducerIntents),
	)

	result := Result{
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
	}

	if r.RetryInjector != nil {
		if err := r.RetryInjector.MaybeFail(scopeValue, generation); err != nil {
			return Result{}, err
		}
	}

	if r.ContentBeforeCanonical {
		contentResult, err := r.writeContentProjection(ctx, scopeValue, projection.contentMaterialization)
		if err != nil {
			return Result{}, err
		}
		result.Content = contentResult
	}

	if err := r.writeCanonicalProjection(ctx, scopeValue, generation.GenerationID, inputFacts, projection.canonical); err != nil {
		return Result{}, err
	}

	if !r.ContentBeforeCanonical {
		contentResult, err := r.writeContentProjection(ctx, scopeValue, projection.contentMaterialization)
		if err != nil {
			return Result{}, err
		}
		result.Content = contentResult
	}

	if len(projection.reducerIntents) > 0 {
		if r.IntentWriter == nil {
			return Result{}, errors.New("reducer intent writer is required when reducer intents are present")
		}

		enqueueStart := time.Now()
		if r.Tracer != nil {
			var enqueueSpan trace.Span
			ctx, enqueueSpan = r.Tracer.Start(ctx, telemetry.SpanReducerIntentEnqueue)
			defer enqueueSpan.End()
		}

		intentResult, err := r.IntentWriter.Enqueue(ctx, projection.reducerIntents)
		if err != nil {
			return Result{}, fmt.Errorf("enqueue reducer intents: %w", err)
		}

		if r.Instruments != nil {
			duration := time.Since(enqueueStart).Seconds()
			r.Instruments.ProjectorStageDuration.Record(ctx, duration, metric.WithAttributes(
				telemetry.AttrScopeKind(string(scopeValue.ScopeKind)),
				attribute.String("stage", "intent_enqueue"),
			))
			r.Instruments.ReducerIntentsEnqueued.Add(ctx, int64(len(projection.reducerIntents)), metric.WithAttributes(
				telemetry.AttrScopeKind(string(scopeValue.ScopeKind)),
			))
		}
		r.logRuntimeStage(
			ctx, scopeValue, generation.GenerationID, "intent_enqueue", enqueueStart,
			"reducer_intent_count", len(projection.reducerIntents),
			"enqueued_count", intentResult.Count,
		)

		result.Intents = intentResult
	}

	return result, nil
}

type projection struct {
	canonical              canonical.CanonicalMaterialization
	contentMaterialization content.Materialization
	reducerIntents         []ReducerIntent
	// quarantinedFacts are the facts a typed canonical extractor skipped during
	// decode because a required identity field was missing or null. Project
	// records them as visible input_invalid dead-letters; they are not projected.
	quarantinedFacts []decode.QuarantinedFact
	// ownedFactCount is how many input facts the projected generation owns;
	// the rest came from the delta's union base (#8779).
	ownedFactCount int
}

func buildProjection(scopeValue scope.IngestionScope, generation scope.ScopeGeneration, inputFacts []facts.Envelope) (projection, error) {
	// Ref-scoped repository scopes are gated from canonical projection. The
	// canonical graph must contain only the default branch; multi-ref overlays
	// belong to their own isolated namespace (epic #5393, enabler #5417).
	// This gate is fail-closed: an unrecognized ref scope MUST NOT silently
	// project into the canonical graph. Return an empty projection with zero
	// canonical rows, zero content rows, and zero reducer intents.
	if scopeValue.ScopeKind == scope.KindRepositoryRef {
		return projection{}, nil
	}
	repoID := scopeRepoID(scopeValue)
	contentMaterialization := content.Materialization{
		RepoID:       repoID,
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		SourceSystem: scopeValue.SourceSystem,
	}
	materializeContent := repoID != ""
	contentMaterialization.FullSnapshot = materializeContent &&
		contentFullSnapshot(scopeValue, generation, inputFacts)
	if contentMaterialization.FullSnapshot {
		contentMaterialization.RetainedPaths = snapshotFilePaths(inputFacts)
	}

	owned := generationOwnedFacts(generation, inputFacts)
	if err := validateUnionScope(generation, owned); err != nil {
		return projection{}, err
	}
	intents := make([]ReducerIntent, 0, len(owned))
	for i := range inputFacts {
		// fact borrows inputFacts[i] instead of deep-cloning it: every consumer
		// below (validateFactBoundary, ValidateFactSchemaVersion,
		// BuildContentRecord, BuildContentEntityRecord, buildRepositoryRefs,
		// projectorentity.BuildSemanticEntityReducerIntent,
		// BuildReducerIntent) only reads
		// fact.Payload/fact.SourceRef, so it is safe to share the caller's
		// Payload map read-only across this loop. Consumers in this loop MUST
		// NOT mutate fact.Payload (or retain a long-lived alias into it) — doing
		// so would corrupt the caller's inputFacts. See
		// TestBuildProjectionDoesNotMutateInputFactPayloads for the regression
		// guard.
		fact := inputFacts[i]
		if err := validateFactBoundary(scopeValue, generation, fact); err != nil {
			return projection{}, err
		}
		if err := decode.ValidateFactSchemaVersion(fact); err != nil {
			return projection{}, err
		}

		if materializeContent {
			if record, ok := BuildContentRecord(fact); ok {
				contentMaterialization.Records = append(contentMaterialization.Records, record)
			}
			if entity, ok := BuildContentEntityRecord(repoID, fact); ok {
				contentMaterialization.Entities = append(contentMaterialization.Entities, entity)
			}
			if refs := buildRepositoryRefs(fact); len(refs) > 0 {
				contentMaterialization.RepositoryRefs = append(contentMaterialization.RepositoryRefs, refs...)
			}
		}
		if fact.GenerationID != generation.GenerationID {
			continue // a union base row materializes but never derives an intent
		}
		if intent, ok := projectorentity.BuildSemanticEntityReducerIntent(fact); ok {
			intents = append(intents, intent)
		}
		if intent, ok := BuildReducerIntent(fact); ok {
			intents = append(intents, intent)
		}
	}
	intents = appendScopeGenerationReducerIntents(intents, scopeValue, generation, owned)

	sort.SliceStable(intents, func(i, j int) bool {
		left := intents[i]
		right := intents[j]
		if left.Domain != right.Domain {
			return left.Domain < right.Domain
		}
		if left.EntityKey != right.EntityKey {
			return left.EntityKey < right.EntityKey
		}
		return left.FactID < right.FactID
	})

	// Build canonical materialization for Neo4j graph writes. A fatal decode
	// failure (an unsupported schema major) fails the whole projection loudly
	// here, behind the per-fact admission gate above: either gate firing means
	// no partial generation is ever written (issue #7671).
	canonical, quarantined, err := canonical.BuildMaterialization(scopeValue, generation, inputFacts)
	if err != nil {
		return projection{}, fmt.Errorf("build canonical materialization: %w", err)
	}

	return projection{
		canonical:              canonical,
		contentMaterialization: contentMaterialization,
		reducerIntents:         intents,
		quarantinedFacts:       quarantined,
		ownedFactCount:         len(owned),
	}, nil
}

func scopeRepoID(scopeValue scope.IngestionScope) string {
	if len(scopeValue.Metadata) == 0 {
		return ""
	}

	return strings.TrimSpace(scopeValue.Metadata["repo_id"])
}
