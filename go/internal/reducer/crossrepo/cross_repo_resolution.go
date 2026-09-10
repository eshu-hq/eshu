// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossrepo //nolint:filelength // 509 lines: cross-repo resolution logic. Consolidating the cross-repo identifier hydration and resolution graph reads in one file keeps the deterministic ordering and dedup rules reviewable.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// CrossRepoEvidenceSource is the evidence_source the cross-repo resolver stamps
// on every edge it writes, including the repo_dependency family's RUNS_ON edges.
//
// Exported because the Ifá assert gate partitions on it: workload materialization
// writes the identical (WorkloadInstance)-[:RUNS_ON]->(Platform) shape stamped
// EvidenceSourceWorkloads, so relationship type and endpoint labels cannot tell
// the two families' edges apart. The gate reads this constant rather than a
// copied literal so the assertion cannot drift from what the writer stamps.
const CrossRepoEvidenceSource = "resolver/cross-repo"

// CrossRepoBackwardEvidenceNotReadyFailureClass classifies a deferral of
// cross-repo resolution while backward evidence has not committed.
//
// Registered as a non-counting reducer retry class
// (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go): the scope is
// waiting on upstream evidence, not failing on its own merits. Returning
// success here instead would terminally strand the scope: the queue item
// succeeds, nothing retries it, and downstream consumers (deployable-unit
// correlation, workload materialization) read the generation's partial-or-absent
// resolved set forever (#6184).
const CrossRepoBackwardEvidenceNotReadyFailureClass = "cross_repo_backward_evidence_not_ready"

// BackwardEvidenceNotReadyError defers resolution until backward evidence
// commits. Retryable so the queue re-runs the scope instead of succeeding
// deferred.
type BackwardEvidenceNotReadyError struct {
	ScopeID      string
	GenerationID string
}

func (e BackwardEvidenceNotReadyError) Error() string {
	return fmt.Sprintf(
		"backward evidence not committed for scope %s generation %s; deferring cross-repo resolution rather than succeeding deferred",
		e.ScopeID,
		e.GenerationID,
	)
}

func (BackwardEvidenceNotReadyError) Retryable() bool { return true }

func (BackwardEvidenceNotReadyError) FailureClass() string {
	return CrossRepoBackwardEvidenceNotReadyFailureClass
}

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
	UpsertIntents(ctx context.Context, rows []sharedintent.Row) error
}

// ScopeRepositoryReader lists the git repository IDs whose facts belong to a
// scope generation. The resolver uses it to attribute each resolved edge to
// the scope that owns its source repository (see partitionResolvedOwnership).
// Nil disables the partition (legacy emit-all); unit tests that construct the
// handler directly leave it nil.
type ScopeRepositoryReader interface {
	ListScopeRepositoryIDs(ctx context.Context, scopeID, generationID string) ([]string, error)
}

// CrossRepoRelationshipHandler resolves cross-repository relationships from
// persisted evidence facts and emits durable repo-dependency projection intents.
//
// The handler runs as part of the deployment_mapping reducer domain. It:
//  1. Loads evidence facts persisted during ingestion
//  2. Loads assertions from the assertion store
//  3. Runs relationships.Resolve() to produce candidates and resolved edges
//  4. Persists candidates and resolved edges for audit trail
//  5. Emits repo-owned shared-projection intents for later canonical writes,
//     attributed to the scope that owns each edge's source repository so the
//     same logical edge is never stamped by two generations (see
//     partitionResolvedOwnership). Activation (step 6) is unaffected: a scope
//     whose emits are all foreign still publishes its generation.
type CrossRepoRelationshipHandler struct {
	EvidenceLoader    EvidenceFactLoader
	Assertions        AssertionLoader
	Persister         ResolutionPersister
	IntentWriter      RepoDependencyIntentWriter
	ReadinessLookup   gpphase.ReadinessLookup
	ReadinessPrefetch gpphase.ReadinessPrefetch
	ScopeRepos        ScopeRepositoryReader
	Tracer            trace.Tracer
	Instruments       *telemetry.Instruments
}

// partitionResolvedOwnership splits resolved edges into the ones this scope
// owns (source repository belongs to one of its own repository facts) and
// foreign ones owned by another scope. Every resolving scope sees the union
// of its own and backward evidence, so without this partition two scopes
// routinely resolve the same logical edge; both rows reach the graph writer,
// which MERGEs by edge identity and lets the generation stamp be
// last-writer-wins. Worker-count scheduling then decides the stamp and the
// byte-exact graph digest diverges across the determinism matrix (#6184: the
// same multi-source→multi-target DEPENDS_ON edge landed stamped
// multi-source in N=1 and multi-target in N=4).
//
// Ownership is single-writer by construction: the source scope's resolver
// always emits (its deployment_mapping item exists by production parity —
// every repo snapshot emits the follow-up), so dropping the foreign copy
// loses no edge. Scopes the reader cannot classify (nil reader, lookup
// error is fatal, empty repository set) keep the legacy emit-all behavior
// rather than silently dropping edges.
// resolveOwnership loads this scope's own repository IDs for the ownership
// partition. It returns enforce=false (legacy emit-all) when no reader is
// wired or the scope holds no git repository facts and therefore cannot be
// classified; a lookup failure is fatal rather than guessed.
func (h *CrossRepoRelationshipHandler) resolveOwnership(
	ctx context.Context,
	scopeID string,
	generationID string,
) (map[string]struct{}, bool, error) {
	if h.ScopeRepos == nil {
		return nil, false, nil
	}
	repoIDs, err := h.ScopeRepos.ListScopeRepositoryIDs(ctx, scopeID, generationID)
	if err != nil {
		return nil, false, err
	}
	own := make(map[string]struct{}, len(repoIDs))
	for _, repoID := range repoIDs {
		if normalized := normalizeReducerRepositoryID(repoID); normalized != "" {
			own[normalized] = struct{}{}
		}
	}
	if len(own) == 0 {
		return nil, false, nil
	}
	return own, true, nil
}

// filterEvidenceFactsBySourceRepos restricts evidence to this scope's own
// repositories so retraction covers only repos this scope may rewrite. With
// enforce=false the input is returned unchanged (legacy behavior).
func filterEvidenceFactsBySourceRepos(
	facts []relationships.EvidenceFact,
	ownRepos map[string]struct{},
	enforce bool,
) []relationships.EvidenceFact {
	if !enforce {
		return facts
	}
	// Allocated, not filtered in place: the caller retains facts for its own
	// logging, and an in-place facts[:0] filter would clobber the backing
	// array out from under it.
	kept := make([]relationships.EvidenceFact, 0, len(facts))
	for _, fact := range facts {
		source := normalizeReducerRepositoryID(fact.SourceRepoID)
		if source == "" {
			kept = append(kept, fact)
			continue
		}
		if _, ok := ownRepos[source]; ok {
			kept = append(kept, fact)
		}
	}
	return kept
}

func partitionResolvedOwnership(
	resolved []relationships.ResolvedRelationship,
	ownRepos map[string]struct{},
	enforce bool,
) (owned, dropped []relationships.ResolvedRelationship) {
	if !enforce {
		return resolved, nil
	}
	for _, relationship := range resolved {
		source := normalizeReducerRepositoryID(relationship.SourceRepoID)
		if source == "" {
			owned = append(owned, relationship)
			continue
		}
		if _, ok := ownRepos[source]; ok {
			owned = append(owned, relationship)
			continue
		}
		dropped = append(dropped, relationship)
	}
	return owned, dropped
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
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanCrossRepoResolution,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, scopeID),
				attribute.String(telemetry.LogKeyGenerationID, generationID),
			),
		)
		defer span.End()
	}

	slog.InfoContext(
		ctx, "cross-repo relationship resolution started",
		log.ScopeID(scopeID),
		log.GenerationID(generationID),
		log.Domain("cross_repo_resolution"),
	)

	readinessLookup := h.ReadinessLookup
	readinessKey, hasReadinessKey := crossRepoBackwardEvidenceReadinessKey(scopeID, generationID)
	if hasReadinessKey && h.ReadinessPrefetch != nil {
		resolvedLookup, err := h.ReadinessPrefetch(
			ctx,
			[]gpphase.PhaseKey{readinessKey},
			gpphase.PhaseBackwardEvidenceCommitted,
		)
		if err != nil {
			return 0, fmt.Errorf("prefetch graph projection readiness: %w", err)
		}
		readinessLookup = resolvedLookup
	}
	if hasReadinessKey && readinessLookup == nil {
		slog.WarnContext(
			ctx, "cross-repo readiness lookup not configured; bypassing backward evidence gate",
			log.ScopeID(scopeID),
			log.GenerationID(generationID),
			slog.String("keyspace", string(gpphase.KeyspaceCrossRepoEvidence)),
			slog.String("phase", string(gpphase.PhaseBackwardEvidenceCommitted)),
		)
	}
	if hasReadinessKey && readinessLookup != nil {
		ready, found := readinessLookup(readinessKey, gpphase.PhaseBackwardEvidenceCommitted)
		if !found || !ready {
			slog.InfoContext(
				ctx, "cross-repo resolution gated",
				log.ScopeID(scopeID),
				log.GenerationID(generationID),
				slog.String("reason", "backward_evidence_not_committed"),
			)
			h.recordDuration(ctx, start, scopeID)
			return 0, BackwardEvidenceNotReadyError{
				ScopeID:      scopeID,
				GenerationID: generationID,
			}
		}
	}

	// Step 1: Load persisted evidence facts.
	evidenceFacts, err := h.EvidenceLoader.ListEvidenceFacts(ctx, generationID)
	if err != nil {
		return 0, fmt.Errorf("load evidence facts for resolution: %w", err)
	}
	if len(evidenceFacts) == 0 {
		// Empty-evidence tombstone path. The retract intents that denormalize the
		// (now-empty) generation MUST commit before the generation is activated,
		// so a publish can never precede durable graph acceptance. See the main
		// path (steps 4-6) for the same fence rationale.
		retractRows := buildResolvedEdgeRetractionIntentRows(
			scopeID,
			nil,
			nil,
			crossRepoContributionSourceRunID(scopeID),
			generationID,
			time.Now().UTC(),
		)
		if len(retractRows) > 0 {
			if err := h.IntentWriter.UpsertIntents(ctx, retractRows); err != nil {
				h.recordActivationFenced(ctx, scopeID, generationID, len(retractRows), err)
				return 0, fmt.Errorf("upsert cross-repo dependency retract intents: %w", err)
			}
		}
		if h.Persister != nil {
			if err := h.Persister.ActivateResolutionGeneration(ctx, generationID, scopeID); err != nil {
				return 0, fmt.Errorf("activate empty resolution generation: %w", err)
			}
		}
		if len(retractRows) == 0 {
			slog.InfoContext(
				ctx, "cross-repo resolution skipped: no evidence",
				log.ScopeID(scopeID),
				log.GenerationID(generationID),
			)
			h.recordDuration(ctx, start, scopeID)
			return 0, nil
		}
		slog.InfoContext(
			ctx, "cross-repo resolution emitted retract intents: no evidence",
			log.ScopeID(scopeID),
			log.GenerationID(generationID),
			slog.Int("intent_count", len(retractRows)),
		)
		h.recordDuration(ctx, start, scopeID)
		return len(retractRows), nil
	}

	evidenceFacts = relationships.DedupeEvidenceFacts(evidenceFacts)

	if h.Instruments != nil {
		h.Instruments.CrossRepoEvidenceLoaded.Add(
			ctx, int64(len(evidenceFacts)),
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

	// Ownership partition (single-writer per edge): keep only the resolved
	// edges sourced in this scope's own repositories, and scope the
	// retraction inputs the same way. See partitionResolvedOwnership.
	ownRepos, enforceOwnership, err := h.resolveOwnership(ctx, scopeID, generationID)
	if err != nil {
		return 0, fmt.Errorf("list scope repositories for resolution ownership: %w", err)
	}
	ownedResolved, droppedResolved := partitionResolvedOwnership(resolved, ownRepos, enforceOwnership)
	ownEvidenceFacts := filterEvidenceFactsBySourceRepos(evidenceFacts, ownRepos, enforceOwnership)
	if len(droppedResolved) > 0 {
		slog.InfoContext(
			ctx, "cross-repo resolution dropped foreign-owned edges",
			log.ScopeID(scopeID),
			log.GenerationID(generationID),
			slog.Int("dropped", len(droppedResolved)),
			slog.Int("owned", len(ownedResolved)),
		)
	}

	slog.InfoContext(
		ctx, "cross-repo relationship resolution completed",
		log.ScopeID(scopeID),
		log.GenerationID(generationID),
		slog.Int("evidence_count", len(evidenceFacts)),
		slog.Int("candidate_count", len(candidates)),
		slog.Int("resolved_count", len(resolved)),
	)

	// Step 4: Persist the resolution audit trail (candidates + resolved rows).
	// Activation is deferred to step 6 so the generation cannot be published to
	// the repo-dependency surface before its durable graph-edge acceptance
	// intents (step 5) have committed. Candidates and resolved rows only become
	// queryable once the generation is activated, so persisting them here is
	// safe ahead of the fence.
	if h.Persister != nil {
		if err := h.Persister.UpsertCandidates(ctx, generationID, candidates); err != nil {
			return 0, fmt.Errorf("persist candidates: %w", err)
		}
		if err := h.Persister.UpsertResolved(ctx, generationID, ownedResolved); err != nil {
			return 0, fmt.Errorf("persist resolved: %w", err)
		}
	}

	// Step 5: Convert resolved relationships to durable repo-dependency intents
	// and commit them BEFORE activation. The repo-owned projection runner
	// reconstructs the full active snapshot for each source repository before
	// touching canonical graph edges. Committing acceptance first fences the
	// publish: if this write fails, the generation stays un-activated, so the
	// retry/reconciler path converges without stranding denormalized edges.
	sourceRunID := crossRepoContributionSourceRunID(scopeID)
	now := time.Now().UTC()
	retractRows := buildResolvedEdgeRetractionIntentRows(
		scopeID,
		ownEvidenceFacts,
		ownedResolved,
		sourceRunID,
		generationID,
		now,
	)
	writeRows, routeCounts := buildResolvedEdgeIntentRows(
		ownedResolved,
		scopeID,
		sourceRunID,
		generationID,
		now,
	)
	intentRows := append(retractRows, writeRows...)
	if len(intentRows) > 0 {
		if err := h.IntentWriter.UpsertIntents(ctx, intentRows); err != nil {
			h.recordActivationFenced(ctx, scopeID, generationID, len(intentRows), err)
			return 0, fmt.Errorf("upsert cross-repo dependency intents: %w", err)
		}
	}

	// Step 6: Activate (publish) the generation now that its graph-acceptance
	// intents are durably committed. This ordering is the publish fence.
	if h.Persister != nil {
		if err := h.Persister.ActivateResolutionGeneration(ctx, generationID, scopeID); err != nil {
			return 0, fmt.Errorf("activate resolution generation: %w", err)
		}
	}

	if len(intentRows) == 0 {
		h.recordDuration(ctx, start, scopeID)
		return 0, nil
	}

	if h.Instruments != nil {
		for relationshipType, count := range routeCounts {
			h.Instruments.CrossRepoEdgesResolved.Add(
				ctx, int64(count),
				metric.WithAttributes(
					attribute.String("relationship_type", relationshipType),
				),
			)
		}
	}

	slog.InfoContext(
		ctx, "cross-repo relationship routing completed",
		log.ScopeID(scopeID),
		log.GenerationID(generationID),
		slog.Any("relationship_route_counts", routeCounts),
		slog.Int("intent_count", len(intentRows)),
		slog.Int("retract_intent_count", len(retractRows)),
	)

	h.recordDuration(ctx, start, scopeID)

	return len(intentRows), nil
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
) (gpphase.PhaseKey, bool) {
	key := gpphase.PhaseKey{
		ScopeID:          strings.TrimSpace(scopeID),
		AcceptanceUnitID: strings.TrimSpace(scopeID),
		SourceRunID:      strings.TrimSpace(generationID),
		GenerationID:     strings.TrimSpace(generationID),
		Keyspace:         gpphase.KeyspaceCrossRepoEvidence,
	}
	if err := key.Validate(); err != nil {
		return gpphase.PhaseKey{}, false
	}
	return key, true
}

// recordDuration records the cross-repo resolution duration metric.
func (h *CrossRepoRelationshipHandler) recordDuration(ctx context.Context, start time.Time, scopeID string) {
	if h.Instruments != nil {
		h.Instruments.CrossRepoResolutionDuration.Record(
			ctx,
			time.Since(start).Seconds(),
		)
	}
}

// recordActivationFenced emits the operator-facing signal that a generation's
// activation was withheld because its durable graph-acceptance intents failed
// to commit. The generation is therefore left un-published so no stranded
// denormalized edges (confidence/generation_id/resolved_id) leak to the
// repo-dependency surface; the reducer retry path converges idempotently and
// the #3559/#3616 reconciler remains as defense-in-depth. The warn log gives an
// at-3-AM operator the scope, generation, withheld intent count, and failure
// class without needing a dashboard.
func (h *CrossRepoRelationshipHandler) recordActivationFenced(
	ctx context.Context,
	scopeID string,
	generationID string,
	intentCount int,
	cause error,
) {
	slog.WarnContext(
		ctx, "cross-repo activation fenced: graph acceptance not durable",
		log.ScopeID(scopeID),
		log.GenerationID(generationID),
		slog.Int("withheld_intent_count", intentCount),
		slog.String("reason", "graph_acceptance_commit_failed"),
		slog.Any("error", cause),
	)
	if h.Instruments != nil {
		h.Instruments.CrossRepoActivationFenced.Add(
			ctx, 1,
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
