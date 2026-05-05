package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const crossRepoEvidenceSource = "resolver/cross-repo"

// EvidenceFactLoader loads persisted evidence facts for a generation.
type EvidenceFactLoader interface {
	ListEvidenceFacts(ctx context.Context, generationID string) ([]relationships.EvidenceFact, error)
}

// AssertionLoader loads relationship assertions.
type AssertionLoader interface {
	ListAssertions(ctx context.Context, relationshipType *relationships.RelationshipType) ([]relationships.Assertion, error)
}

// ResolutionPersister persists resolution outputs (candidates and resolved
// relationships) for audit trail and activates the generation so downstream
// consumers (e.g. workload materialization) can query resolved relationships.
type ResolutionPersister interface {
	UpsertCandidates(ctx context.Context, generationID string, candidates []relationships.Candidate) error
	UpsertResolved(ctx context.Context, generationID string, resolved []relationships.ResolvedRelationship) error
	ActivateResolutionGeneration(ctx context.Context, generationID, scopeID string) error
}

// RepoDependencyIntentWriter persists durable repo-dependency projection
// intents plus their authoritative acceptance rows.
type RepoDependencyIntentWriter interface {
	UpsertIntents(ctx context.Context, rows []SharedProjectionIntentRow) error
}

// CrossRepoRelationshipHandler resolves cross-repository relationships from
// persisted evidence facts and emits durable repo-dependency projection intents.
//
// The handler runs as part of the deployment_mapping reducer domain. It:
//  1. Loads evidence facts persisted during ingestion
//  2. Loads assertions from the assertion store
//  3. Runs relationships.Resolve() to produce candidates and resolved edges
//  4. Persists candidates and resolved edges for audit trail
//  5. Emits repo-owned shared-projection intents for later canonical writes
type CrossRepoRelationshipHandler struct {
	EvidenceLoader    EvidenceFactLoader
	Assertions        AssertionLoader
	Persister         ResolutionPersister
	IntentWriter      RepoDependencyIntentWriter
	ReadinessLookup   GraphProjectionReadinessLookup
	ReadinessPrefetch GraphProjectionReadinessPrefetch
	Tracer            trace.Tracer
	Instruments       *telemetry.Instruments
}

// Resolve executes the cross-repo relationship resolution pipeline for one
// generation. Returns the number of durable intents emitted.
func (h *CrossRepoRelationshipHandler) Resolve(
	ctx context.Context,
	scopeID string,
	generationID string,
) (int, error) {
	if h.EvidenceLoader == nil || h.IntentWriter == nil {
		return 0, nil
	}

	start := time.Now()

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(ctx, telemetry.SpanCrossRepoResolution,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, scopeID),
				attribute.String(telemetry.LogKeyGenerationID, generationID),
			),
		)
		defer span.End()
	}

	slog.InfoContext(ctx, "cross-repo relationship resolution started",
		slog.String(telemetry.LogKeyScopeID, scopeID),
		slog.String(telemetry.LogKeyGenerationID, generationID),
		slog.String(telemetry.LogKeyDomain, "cross_repo_resolution"),
	)

	readinessLookup := h.ReadinessLookup
	readinessKey, hasReadinessKey := crossRepoBackwardEvidenceReadinessKey(scopeID, generationID)
	if hasReadinessKey && h.ReadinessPrefetch != nil {
		resolvedLookup, err := h.ReadinessPrefetch(
			ctx,
			[]GraphProjectionPhaseKey{readinessKey},
			GraphProjectionPhaseBackwardEvidenceCommitted,
		)
		if err != nil {
			return 0, fmt.Errorf("prefetch graph projection readiness: %w", err)
		}
		readinessLookup = resolvedLookup
	}
	if hasReadinessKey && readinessLookup == nil {
		slog.WarnContext(ctx, "cross-repo readiness lookup not configured; bypassing backward evidence gate",
			slog.String(telemetry.LogKeyScopeID, scopeID),
			slog.String(telemetry.LogKeyGenerationID, generationID),
			slog.String("keyspace", string(GraphProjectionKeyspaceCrossRepoEvidence)),
			slog.String("phase", string(GraphProjectionPhaseBackwardEvidenceCommitted)),
		)
	}
	if hasReadinessKey && readinessLookup != nil {
		ready, found := readinessLookup(readinessKey, GraphProjectionPhaseBackwardEvidenceCommitted)
		if !found || !ready {
			slog.InfoContext(ctx, "cross-repo resolution gated",
				slog.String(telemetry.LogKeyScopeID, scopeID),
				slog.String(telemetry.LogKeyGenerationID, generationID),
				slog.String("reason", "backward_evidence_not_committed"),
			)
			h.recordDuration(ctx, start, scopeID)
			return 0, nil
		}
	}

	// Step 1: Load persisted evidence facts.
	evidenceFacts, err := h.EvidenceLoader.ListEvidenceFacts(ctx, generationID)
	if err != nil {
		return 0, fmt.Errorf("load evidence facts for resolution: %w", err)
	}
	if len(evidenceFacts) == 0 {
		slog.InfoContext(ctx, "cross-repo resolution skipped: no evidence",
			slog.String(telemetry.LogKeyScopeID, scopeID),
			slog.String(telemetry.LogKeyGenerationID, generationID),
		)
		return 0, nil
	}

	evidenceFacts = relationships.DedupeEvidenceFacts(evidenceFacts)

	if h.Instruments != nil {
		h.Instruments.CrossRepoEvidenceLoaded.Add(ctx, int64(len(evidenceFacts)),
			metric.WithAttributes(telemetry.AttrScopeID(scopeID)),
		)
	}

	// Step 2: Load assertions.
	var assertions []relationships.Assertion
	if h.Assertions != nil {
		assertions, err = h.Assertions.ListAssertions(ctx, nil)
		if err != nil {
			return 0, fmt.Errorf("load assertions for resolution: %w", err)
		}
	}

	// Step 3: Resolve.
	candidates, resolved := relationships.Resolve(
		evidenceFacts,
		assertions,
		relationships.DefaultConfidenceThreshold,
	)
	candidates = normalizeRelationshipCandidates(candidates)
	resolved = normalizeResolvedRelationships(resolved)

	slog.InfoContext(ctx, "cross-repo relationship resolution completed",
		slog.String(telemetry.LogKeyScopeID, scopeID),
		slog.String(telemetry.LogKeyGenerationID, generationID),
		slog.Int("evidence_count", len(evidenceFacts)),
		slog.Int("candidate_count", len(candidates)),
		slog.Int("resolved_count", len(resolved)),
	)

	// Step 4: Persist audit trail and activate generation for downstream queries.
	if h.Persister != nil {
		if err := h.Persister.UpsertCandidates(ctx, generationID, candidates); err != nil {
			return 0, fmt.Errorf("persist candidates: %w", err)
		}
		if err := h.Persister.UpsertResolved(ctx, generationID, resolved); err != nil {
			return 0, fmt.Errorf("persist resolved: %w", err)
		}
		if err := h.Persister.ActivateResolutionGeneration(ctx, generationID, scopeID); err != nil {
			return 0, fmt.Errorf("activate resolution generation: %w", err)
		}
	}

	// Step 5: Convert resolved relationships to durable repo-dependency intents.
	// The repo-owned projection runner reconstructs the full active snapshot for
	// each source repository before touching canonical graph edges.
	if len(resolved) == 0 {
		h.recordDuration(ctx, start, scopeID)
		return 0, nil
	}

	writeRows, routeCounts := buildResolvedEdgeIntentRows(
		resolved,
		scopeID,
		crossRepoContributionSourceRunID(scopeID),
		generationID,
	)
	if len(writeRows) == 0 {
		h.recordDuration(ctx, start, scopeID)
		return 0, nil
	}
	if err := h.IntentWriter.UpsertIntents(ctx, writeRows); err != nil {
		return 0, fmt.Errorf("upsert cross-repo dependency intents: %w", err)
	}

	if h.Instruments != nil {
		for relationshipType, count := range routeCounts {
			h.Instruments.CrossRepoEdgesResolved.Add(ctx, int64(count),
				metric.WithAttributes(
					telemetry.AttrScopeID(scopeID),
					attribute.String("relationship_type", relationshipType),
				),
			)
		}
	}

	slog.InfoContext(ctx, "cross-repo relationship routing completed",
		slog.String(telemetry.LogKeyScopeID, scopeID),
		slog.String(telemetry.LogKeyGenerationID, generationID),
		slog.Any("relationship_route_counts", routeCounts),
		slog.Int("intent_count", len(writeRows)),
	)

	h.recordDuration(ctx, start, scopeID)

	return len(writeRows), nil
}

func normalizeRelationshipCandidates(candidates []relationships.Candidate) []relationships.Candidate {
	if len(candidates) == 0 {
		return nil
	}

	normalized := make([]relationships.Candidate, len(candidates))
	for i, candidate := range candidates {
		candidate.SourceRepoID = normalizeReducerRepositoryID(candidate.SourceRepoID)
		candidate.TargetRepoID = normalizeReducerRepositoryID(candidate.TargetRepoID)
		normalized[i] = candidate
	}
	return normalized
}

func normalizeResolvedRelationships(
	resolved []relationships.ResolvedRelationship,
) []relationships.ResolvedRelationship {
	if len(resolved) == 0 {
		return nil
	}

	normalized := make([]relationships.ResolvedRelationship, len(resolved))
	for i, relationship := range resolved {
		relationship.SourceRepoID = normalizeReducerRepositoryID(relationship.SourceRepoID)
		relationship.TargetRepoID = normalizeReducerRepositoryID(relationship.TargetRepoID)
		normalized[i] = relationship
	}
	return normalized
}

func normalizeReducerRepositoryID(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.Index(value, "repository:"); idx > 0 {
		prefix := value[:idx]
		if strings.HasSuffix(prefix, "scope:") {
			return value[idx:]
		}
	}
	return value
}

func crossRepoBackwardEvidenceReadinessKey(
	scopeID string,
	generationID string,
) (GraphProjectionPhaseKey, bool) {
	key := GraphProjectionPhaseKey{
		ScopeID:          strings.TrimSpace(scopeID),
		AcceptanceUnitID: strings.TrimSpace(scopeID),
		SourceRunID:      strings.TrimSpace(generationID),
		GenerationID:     strings.TrimSpace(generationID),
		Keyspace:         GraphProjectionKeyspaceCrossRepoEvidence,
	}
	if err := key.Validate(); err != nil {
		return GraphProjectionPhaseKey{}, false
	}
	return key, true
}

// recordDuration records the cross-repo resolution duration metric.
func (h *CrossRepoRelationshipHandler) recordDuration(ctx context.Context, start time.Time, scopeID string) {
	if h.Instruments != nil {
		h.Instruments.CrossRepoResolutionDuration.Record(ctx,
			time.Since(start).Seconds(),
			metric.WithAttributes(telemetry.AttrScopeID(scopeID)),
		)
	}
}

func crossRepoContributionSourceRunID(scopeID string) string {
	scopeID = strings.TrimSpace(scopeID)
	if scopeID == "" {
		return "repo_dependency"
	}
	return "repo_dependency:" + scopeID
}

// buildResolvedEdgeIntentRows converts resolved relationships to shared
// projection intent rows while preserving typed relationship families.
func buildResolvedEdgeIntentRows(
	resolved []relationships.ResolvedRelationship,
	scopeID string,
	sourceRunID string,
	generationID string,
) ([]SharedProjectionIntentRow, map[string]int) {
	now := time.Now().UTC()
	rows := make([]SharedProjectionIntentRow, 0, len(resolved))
	routeCounts := make(map[string]int)

	for i, r := range resolved {
		row, routeType, ok := buildResolvedEdgeIntentRow(
			r,
			scopeID,
			sourceRunID,
			generationID,
			i,
			now,
		)
		if !ok {
			continue
		}
		rows = append(rows, row)
		routeCounts[routeType]++
	}

	return rows, routeCounts
}

func buildResolvedEdgeIntentRow(
	r relationships.ResolvedRelationship,
	scopeID string,
	sourceRunID string,
	generationID string,
	ordinal int,
	createdAt time.Time,
) (SharedProjectionIntentRow, string, bool) {
	if r.SourceRepoID == "" {
		return SharedProjectionIntentRow{}, "", false
	}

	payload := map[string]any{
		"repo_id":           r.SourceRepoID,
		"evidence_source":   crossRepoEvidenceSource,
		"resolved_id":       relationships.ResolvedRelationshipID(generationID, r, ordinal),
		"generation_id":     generationID,
		"confidence":        r.Confidence,
		"evidence_count":    r.EvidenceCount,
		"evidence_kinds":    toStringSlice(r.Details["evidence_kinds"]),
		"rationale":         r.Rationale,
		"resolution_source": string(r.ResolutionSource),
	}
	if evidenceType := resolvedRelationshipEvidenceType(r); evidenceType != "" {
		payload["evidence_type"] = evidenceType
	}
	if artifacts := resolvedRelationshipEvidenceArtifacts(r); len(artifacts) > 0 {
		payload["evidence_artifacts"] = artifacts
	}

	partitionKey := ""
	routeType := string(r.RelationshipType)

	switch r.RelationshipType {
	case relationships.RelRunsOn:
		if r.TargetEntityID == "" {
			return SharedProjectionIntentRow{}, "", false
		}
		payload["platform_id"] = r.TargetEntityID
		payload["relationship_type"] = string(r.RelationshipType)
		partitionKey = fmt.Sprintf("runs_on:%s->%s", r.SourceRepoID, r.TargetEntityID)
	case relationships.RelDeploysFrom, relationships.RelDiscoversConfigIn, relationships.RelProvisionsDependencyFor:
		if r.TargetRepoID == "" {
			return SharedProjectionIntentRow{}, "", false
		}
		payload["target_repo_id"] = r.TargetRepoID
		payload["relationship_type"] = string(r.RelationshipType)
		partitionKey = fmt.Sprintf("repo:%s->%s|%s", r.SourceRepoID, r.TargetRepoID, r.RelationshipType)
	default:
		if r.TargetRepoID == "" {
			return SharedProjectionIntentRow{}, "", false
		}
		payload["target_repo_id"] = r.TargetRepoID
		payload["relationship_type"] = string(r.RelationshipType)
		partitionKey = fmt.Sprintf("repo:%s->%s|%s", r.SourceRepoID, r.TargetRepoID, r.RelationshipType)
	}

	return BuildSharedProjectionIntent(SharedProjectionIntentInput{
		ProjectionDomain: DomainRepoDependency,
		PartitionKey:     partitionKey,
		ScopeID:          scopeID,
		AcceptanceUnitID: r.SourceRepoID,
		RepositoryID:     r.SourceRepoID,
		SourceRunID:      strings.TrimSpace(sourceRunID),
		GenerationID:     generationID,
		Payload:          payload,
		CreatedAt:        createdAt,
	}), routeType, true
}
