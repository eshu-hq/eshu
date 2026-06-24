// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// CanonicalWriter writes canonical Neo4j nodes from a CanonicalMaterialization.
type CanonicalWriter interface {
	Write(context.Context, CanonicalMaterialization) error
}

type Runtime struct {
	CanonicalWriter CanonicalWriter // replaces GraphWriter — canonical graph projection
	ContentWriter   content.Writer
	IntentWriter    ReducerIntentWriter
	PhasePublisher  reducer.GraphProjectionPhasePublisher
	RepairQueue     reducer.GraphProjectionPhaseRepairQueue
	RetryInjector   RetryInjector
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

type ReducerIntent struct {
	ScopeID      string
	GenerationID string
	Domain       reducer.Domain
	EntityKey    string
	Reason       string
	FactID       string
	SourceSystem string
	Payload      map[string]any
}

func (i ReducerIntent) ScopeGenerationKey() string {
	return fmt.Sprintf("%s:%s", i.ScopeID, i.GenerationID)
}

type IntentResult struct {
	Count int
}

type ReducerIntentWriter interface {
	Enqueue(context.Context, []ReducerIntent) (IntentResult, error)
}

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

	buildStart := time.Now()
	projection, err := buildProjection(scopeValue, generation, inputFacts)
	if err != nil {
		return Result{}, err
	}
	if r.Instruments != nil {
		r.Instruments.ProjectorStageDuration.Record(ctx, time.Since(buildStart).Seconds(), metric.WithAttributes(
			telemetry.AttrScopeID(scopeValue.ScopeID),
			attribute.String("stage", "build_projection"),
		))
	}
	r.logRuntimeStage(
		ctx, scopeValue, generation.GenerationID, "build_projection", buildStart,
		"fact_count", len(inputFacts),
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
				telemetry.AttrScopeID(scopeValue.ScopeID),
				attribute.String("stage", "intent_enqueue"),
			))
			r.Instruments.ReducerIntentsEnqueued.Add(ctx, int64(len(projection.reducerIntents)), metric.WithAttributes(
				telemetry.AttrScopeID(scopeValue.ScopeID),
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
	canonical              CanonicalMaterialization
	contentMaterialization content.Materialization
	reducerIntents         []ReducerIntent
}

func buildProjection(scopeValue scope.IngestionScope, generation scope.ScopeGeneration, inputFacts []facts.Envelope) (projection, error) {
	repoID := scopeRepoID(scopeValue)
	contentMaterialization := content.Materialization{
		RepoID:       repoID,
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		SourceSystem: scopeValue.SourceSystem,
	}
	materializeContent := repoID != ""

	intents := make([]ReducerIntent, 0, len(inputFacts))
	for i := range inputFacts {
		fact := inputFacts[i].Clone()
		if err := validateFactBoundary(scopeValue, generation, fact); err != nil {
			return projection{}, err
		}
		if err := validateFactSchemaVersion(fact); err != nil {
			return projection{}, err
		}

		if materializeContent {
			if record, ok := buildContentRecord(fact); ok {
				contentMaterialization.Records = append(contentMaterialization.Records, record)
			}
			if entity, ok := buildContentEntityRecord(repoID, fact); ok {
				contentMaterialization.Entities = append(contentMaterialization.Entities, entity)
			}
			if refs := buildRepositoryRefs(fact); len(refs) > 0 {
				contentMaterialization.RepositoryRefs = append(contentMaterialization.RepositoryRefs, refs...)
			}
		}
		if intent, ok := buildSemanticEntityReducerIntent(fact); ok {
			intents = append(intents, intent)
		}
		if intent, ok := buildReducerIntent(fact); ok {
			intents = append(intents, intent)
		}
	}
	intents = appendScopeGenerationReducerIntents(intents, scopeValue, generation, inputFacts)

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

	// Build canonical materialization for Neo4j graph writes.
	canonical := buildCanonicalMaterialization(scopeValue, generation, inputFacts)

	return projection{
		canonical:              canonical,
		contentMaterialization: contentMaterialization,
		reducerIntents:         intents,
	}, nil
}

func scopeRepoID(scopeValue scope.IngestionScope) string {
	if len(scopeValue.Metadata) == 0 {
		return ""
	}

	return strings.TrimSpace(scopeValue.Metadata["repo_id"])
}

func validateFactBoundary(scopeValue scope.IngestionScope, generation scope.ScopeGeneration, fact facts.Envelope) error {
	if fact.ScopeID != scopeValue.ScopeID {
		return fmt.Errorf("fact %q scope_id %q does not match scope %q", fact.FactID, fact.ScopeID, scopeValue.ScopeID)
	}
	if fact.GenerationID != generation.GenerationID {
		return fmt.Errorf("fact %q generation_id %q does not match generation %q", fact.FactID, fact.GenerationID, generation.GenerationID)
	}

	return nil
}

func buildContentRecord(fact facts.Envelope) (content.Record, bool) {
	path, ok := payloadString(fact.Payload, "content_path")
	if !ok {
		return content.Record{}, false
	}
	if !payloadHasKey(fact.Payload, "content_body") && !payloadHasKey(fact.Payload, "content_digest") {
		return content.Record{}, false
	}

	body, _ := payloadString(fact.Payload, "content_body")
	digest, _ := payloadString(fact.Payload, "content_digest")

	return content.Record{
		Path:     path,
		Body:     body,
		Digest:   digest,
		Deleted:  fact.IsTombstone,
		Metadata: payloadAttributes(fact.Payload, "content_path", "content_body", "content_digest"),
	}, true
}

func buildContentEntityRecord(repoID string, fact facts.Envelope) (content.EntityRecord, bool) {
	relativePath, ok := payloadString(fact.Payload, "content_path")
	if !ok {
		relativePath, ok = payloadString(fact.Payload, "relative_path")
	}
	if !ok {
		relativePath, ok = payloadString(fact.Payload, "path")
	}
	if !ok {
		return content.EntityRecord{}, false
	}

	entityType, ok := payloadString(fact.Payload, "entity_kind")
	if !ok {
		entityType, ok = payloadString(fact.Payload, "entity_type")
	}
	if !ok {
		entityType, ok = payloadString(fact.Payload, "sql_entity_type")
	}
	if !ok {
		entityType = fact.FactKind
	}
	if strings.TrimSpace(entityType) == "" {
		return content.EntityRecord{}, false
	}

	entityName, ok := payloadString(fact.Payload, "entity_name")
	if !ok {
		entityName, ok = payloadString(fact.Payload, "name")
	}
	if !ok {
		return content.EntityRecord{}, false
	}

	startLine, ok := payloadInt(fact.Payload, "start_line")
	if !ok {
		startLine, ok = payloadInt(fact.Payload, "line_number")
	}
	if !ok || startLine <= 0 {
		startLine = 1
	}

	endLine, ok := payloadInt(fact.Payload, "end_line")
	if !ok || endLine < startLine {
		endLine = startLine
	}

	startByte := payloadIntPtr(fact.Payload, "start_byte")
	endByte := payloadIntPtr(fact.Payload, "end_byte")
	language, _ := payloadString(fact.Payload, "language")
	if language == "" {
		language, _ = payloadString(fact.Payload, "lang")
	}
	artifactType, _ := payloadString(fact.Payload, "artifact_type")
	templateDialect, _ := payloadString(fact.Payload, "template_dialect")
	iacRelevant := payloadBoolPtr(fact.Payload, "iac_relevant")
	entityID, ok := payloadString(fact.Payload, "entity_id")
	if !ok {
		entityID = content.CanonicalEntityID(repoID, relativePath, entityType, entityName, startLine)
	}
	sourceCache, _ := payloadString(fact.Payload, "source_cache")

	return content.EntityRecord{
		EntityID:        entityID,
		Path:            relativePath,
		EntityType:      entityType,
		EntityName:      entityName,
		StartLine:       startLine,
		EndLine:         endLine,
		StartByte:       startByte,
		EndByte:         endByte,
		Language:        language,
		ArtifactType:    artifactType,
		TemplateDialect: templateDialect,
		IACRelevant:     iacRelevant,
		SourceCache:     sourceCache,
		Metadata:        entityMetadataFromPayload(fact.Payload),
		Deleted:         fact.IsTombstone,
	}, true
}
