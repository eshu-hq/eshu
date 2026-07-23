// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// crossplaneSatisfiedByMaterializationDomainDefinition returns the additive
// definition for the Crossplane Claim -> XRD SATISFIED_BY edge projection. It
// is additive (not part of DefaultDomainDefinitions) because the handler
// requires an explicitly wired CrossplaneSatisfiedByEdgeWriter and
// FactLoader; registering it without them would silently drop every intent.
// Mirrors kubernetesCorrelationMaterializationDomainDefinition. See issue
// #5347.
func crossplaneSatisfiedByMaterializationDomainDefinition() DomainDefinition {
	return DomainDefinition{
		Domain:  DomainCrossplaneSatisfiedByMaterialization,
		Summary: "project Crossplane Claim -> XRD classification decisions into canonical SATISFIED_BY graph edges",
		Ownership: OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "crossplane_satisfied_by_materialization",
			SourceLayers: []truth.Layer{
				truth.LayerObservedResource,
			},
		},
	}
}

// crossplaneSatisfiedByEdgeEvidenceSource tags SATISFIED_BY edges written by
// this reducer so the prior-generation retract path scopes its delete to
// reducer-owned Crossplane classification edges and never touches edges
// owned by another writer. Must match
// cypher.crossplaneSatisfiedByEvidenceSource byte-for-byte (the two packages
// each own their own constant, mirroring
// kubernetesCorrelationEdgeEvidenceSource).
const crossplaneSatisfiedByEdgeEvidenceSource = "reducer/crossplane-satisfied-by"

// CrossplaneSatisfiedByEdgeWriter persists and retracts canonical
// SATISFIED_BY edges between a K8sResource node (the Claim) and the
// CrossplaneXRD node it resolved against. Implementations MUST be idempotent
// by (claim uid, SATISFIED_BY, xrd uid) so reducer retries and duplicate
// facts converge on one edge, and MUST NOT fabricate endpoint nodes: a row
// whose claim or XRD node is absent is a no-op. Mirrors
// KubernetesCorrelationEdgeWriter.
type CrossplaneSatisfiedByEdgeWriter interface {
	WriteCrossplaneSatisfiedByEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
	RetractCrossplaneSatisfiedByEdges(ctx context.Context, scopeIDs []string, generationID string, evidenceSource string) error
}

// crossplaneXRDFactLoader loads the cross-scope active CrossplaneXRD
// content-entity facts a Claim candidate resolves against. XRDs commonly live
// in a platform repo separate from the Claims that reference them, so they
// are loaded across scopes exactly like kubernetesCorrelationSourceFactLoader
// loads cross-scope OCI source evidence.
type crossplaneXRDFactLoader interface {
	ListActiveCrossplaneXRDFacts(ctx context.Context) ([]facts.Envelope, error)
}

// CrossplaneRedriveTargetLedgerWriter records a target Claim scope as
// confirmed satisfied for one XRD (group, claim_kind) identity (issue
// #5476). postgres.CrossplaneRedriveTargetLedgerStore.RecordRedriven
// implements this with the identical signature. Implementations MUST be
// idempotent (ON CONFLICT DO NOTHING or equivalent): the handler calls this
// on every successful materialization that resolves at least one edge for
// the identity, including re-runs after a retry or a re-projected
// generation, and the cross-scope redrive sweep's target-discovery query
// relies on a recorded pair staying recorded forever.
type CrossplaneRedriveTargetLedgerWriter interface {
	RecordRedriven(ctx context.Context, targetScopeID, group, claimKind string) error
}

// CrossplaneSatisfiedByMaterializationHandler reduces one Crossplane
// classification materialization intent into canonical SATISFIED_BY edge
// writes. It loads the intent's own scope generation's content_entity facts
// (Claim candidates: K8sResource rows) plus the cross-scope active
// CrossplaneXRD facts, resolves each candidate's (group, kind) against
// exactly one XRD's (spec.group, spec.claimNames.kind), and hands the
// resulting batch to the edge writer. A zero-match candidate is an ordinary
// Kubernetes object and an ambiguous (2+ match) candidate stays
// provenance-only; neither fabricates an edge.
//
// Deviation from the locked design's dual-keyspace readiness gate (reviewed
// and accepted, issue #5347): this handler does NOT gate on a
// canonical-nodes-committed phase before resolving, for two independent
// reasons — one that makes the gate unnecessary for the same-scope case, and
// one that makes it near-impossible to violate even cross-scope.
//
//  1. Same-scope (the common case: Claim and XRD in the same repo). Both
//     endpoints are projector-canonical nodes. internal/projector/runtime.go's
//     Project writes the canonical projection (including any CrossplaneXRD and
//     K8sResource nodes for this generation) at its writeCanonicalProjection
//     call BEFORE it enqueues reducer intents at its IntentWriter.Enqueue call
//     later in the same function. So by the time this handler's intent is even
//     enqueued, both endpoint nodes for this scope's generation are already
//     committed — there is no race to gate against, and a readiness gate here
//     would only add latency for a hazard that cannot occur.
//  2. Cross-scope (the XRD lives in a different, commonly platform, repo).
//     internal/storage/postgres's listActiveCrossplaneXRDFactsQuery joins on
//     scope.active_generation_id, and a generation becomes active only inside
//     ProjectorQueue.Ack — called strictly after Project() (and therefore
//     after that repo's own canonical write) returns successfully
//     (internal/projector/service.go). So any CrossplaneXRD fact this handler
//     loads cross-scope already has its node committed in the graph; a
//     cross-scope MATCH-miss from an uncommitted XRD node is near-impossible
//     by this activation ordering alone, not merely tolerated as a residual
//     risk.
//
// The GraphProjectionKeyspaceCodeEntitiesUID phase that a literal port of the
// locked design's dual-keyspace gate would need is therefore both unnecessary
// (case 1) and would require reconstructing a cross-repo phase key this
// handler cannot derive without risking a permanently-mismatched,
// always-not-ready gate (case 2) — worse than the gap it would close. The one
// residual gap neither argument above closes is XRD-lag: an XRD repo
// ingested for the FIRST time after the Claim repo's latest generation has no
// active generation yet, so the Claim's correlation finds no XRD candidate
// until the Claim repo's own next sync generation re-runs it — a false
// negative, not a wrong answer, and not a same-generation race. Tracked for a
// periodic re-drive in issue #5476. Safety is preserved by construction on
// top of both arguments: the writer's MATCH-MATCH-MERGE only ever produces an
// edge when both endpoint nodes already exist in the graph, so even an
// unforeseen uncommitted endpoint yields no edge this generation (self-healing
// on a later retry) rather than a fabricated one.
type CrossplaneSatisfiedByMaterializationHandler struct {
	FactLoader  FactLoader
	EdgeWriter  CrossplaneSatisfiedByEdgeWriter
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	// PriorGenerationCheck reports whether the scope has any prior
	// generation. Nil keeps retract behavior conservative (always retract
	// before write).
	PriorGenerationCheck PriorGenerationCheck
	// RedriveTargetLedger records intent.ScopeID as confirmed satisfied for
	// each (group, claim_kind) identity this run actually resolved an edge
	// for (issue #5476). Nil is safe (no-op): the cross-scope redrive
	// sweep's already-satisfied fence then never suppresses (every matching
	// sweep re-enqueues), which is correct-but-wasteful, never
	// incorrect-and-silent.
	RedriveTargetLedger CrossplaneRedriveTargetLedgerWriter
	// EdgeExistenceReader confirms which resolved rows actually have a
	// committed SATISFIED_BY edge after WriteCrossplaneSatisfiedByEdges
	// returns (issue #5476 P1-b): the writer's MATCH-MATCH-MERGE
	// deliberately no-ops (nil error, no edge) when an endpoint node is
	// absent, so a nil write error alone does not prove a row's edge
	// committed. Nil is safe in the conservative direction: no row is ever
	// confirmed, so RedriveTargetLedger is never written for an unconfirmed
	// row (see confirmWrittenCrossplaneSatisfiedByEdges's doc comment) --
	// correct-but-wasteful, never a false "satisfied" fence.
	EdgeExistenceReader GraphQueryRunner
}

// Handle executes one Crossplane SATISFIED_BY materialization intent.
func (h CrossplaneSatisfiedByMaterializationHandler) Handle(
	ctx context.Context,
	intent Intent,
) (Result, error) {
	totalStart := time.Now()
	if intent.Domain != DomainCrossplaneSatisfiedByMaterialization {
		return Result{}, fmt.Errorf(
			"crossplane satisfied-by materialization handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("crossplane satisfied-by materialization fact loader is required")
	}
	if h.EdgeWriter == nil {
		return Result{}, fmt.Errorf("crossplane satisfied-by materialization edge writer is required")
	}

	if h.Tracer != nil {
		var span trace.Span
		ctx, span = h.Tracer.Start(
			ctx, telemetry.SpanReducerCrossplaneSatisfiedByMaterialization,
			trace.WithAttributes(
				attribute.String(telemetry.LogKeyScopeID, intent.ScopeID),
				attribute.String(telemetry.LogKeyGenerationID, intent.GenerationID),
			),
		)
		defer span.End()
	}

	loadStart := time.Now()
	envelopes, err := h.loadEdgeFacts(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	loadDuration := time.Since(loadStart)

	extractStart := time.Now()
	rows, tally, err := ExtractCrossplaneSatisfiedByEdgeRows(envelopes)
	if err != nil {
		return Result{}, err
	}
	extractDuration := time.Since(extractStart)

	skipRetract, err := h.shouldSkipRetract(ctx, intent)
	if err != nil {
		return Result{}, err
	}
	var retractDuration time.Duration
	if !skipRetract {
		retractStart := time.Now()
		if err := h.EdgeWriter.RetractCrossplaneSatisfiedByEdges(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			crossplaneSatisfiedByEdgeEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("retract canonical crossplane satisfied-by edges: %w", err)
		}
		retractDuration = time.Since(retractStart)
	}

	var writeDuration time.Duration
	if len(rows) > 0 {
		writeStart := time.Now()
		if err := h.EdgeWriter.WriteCrossplaneSatisfiedByEdges(
			ctx,
			rows,
			intent.ScopeID,
			intent.GenerationID,
			crossplaneSatisfiedByEdgeEvidenceSource,
		); err != nil {
			return Result{}, fmt.Errorf("write canonical crossplane satisfied-by edges: %w", err)
		}
		writeDuration = time.Since(writeStart)
		h.recordRedriveLedgerForConfirmedEdges(ctx, intent.ScopeID, rows)
	}

	h.recordEdgeCounter(ctx, rows)
	logCrossplaneSatisfiedByMaterializationCompleted(ctx, crossplaneSatisfiedByMaterializationTiming{
		intent:           intent,
		factCount:        len(envelopes),
		edgeCount:        len(rows),
		ambiguousSkipped: tally.ambiguousSkipped,
		skipRetract:      skipRetract,
		loadDuration:     loadDuration,
		extractDuration:  extractDuration,
		retractDuration:  retractDuration,
		writeDuration:    writeDuration,
		totalDuration:    time.Since(totalStart),
	})

	return Result{
		IntentID: intent.IntentID,
		Domain:   DomainCrossplaneSatisfiedByMaterialization,
		Status:   ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"materialized %d SATISFIED_BY edge(s) from %d fact(s); %d candidate(s) matched 2+ XRDs (ambiguous, skipped)",
			len(rows),
			len(envelopes),
			tally.ambiguousSkipped,
		),
		CanonicalWrites: len(rows),
	}, nil
}

// loadEdgeFacts loads the intent's own scope generation's content_entity
// facts (Claim candidates) and appends the cross-scope active CrossplaneXRD
// facts, mirroring KubernetesCorrelationMaterializationHandler.loadEdgeFacts.
func (h CrossplaneSatisfiedByMaterializationHandler) loadEdgeFacts(
	ctx context.Context,
	intent Intent,
) ([]facts.Envelope, error) {
	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{factKindContentEntity},
	)
	if err != nil {
		return nil, fmt.Errorf("load facts for crossplane satisfied-by materialization: %w", err)
	}
	xrdFacts, err := h.loadActiveXRDFacts(ctx)
	if err != nil {
		return nil, fmt.Errorf("load active crossplane xrd facts: %w", err)
	}
	return append(envelopes, xrdFacts...), nil
}

// loadActiveXRDFacts loads the cross-scope active CrossplaneXRD facts through
// the optional crossplaneXRDFactLoader interface. A loader that does not
// implement it yields no XRD facts (and therefore no edges), never an error.
func (h CrossplaneSatisfiedByMaterializationHandler) loadActiveXRDFacts(ctx context.Context) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(crossplaneXRDFactLoader)
	if !ok {
		return nil, nil
	}
	envelopes, err := loader.ListActiveCrossplaneXRDFacts(ctx)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

// shouldSkipRetract mirrors KubernetesCorrelationMaterializationHandler:
// skip the prior-edge retract on the very first generation for a scope (no
// prior edges to remove) and only on the first attempt.
func (h CrossplaneSatisfiedByMaterializationHandler) shouldSkipRetract(ctx context.Context, intent Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for crossplane satisfied-by retract: %w", err)
	}
	return !hasPrior, nil
}

// recordRedriveLedgerForConfirmedEdges confirms which of rows actually have a
// committed SATISFIED_BY edge (issue #5476 P1-b) and records the ledger only
// for that confirmed subset, never for a row whose write no-oped on an
// absent endpoint. Skips the existence-check read entirely when
// RedriveTargetLedger is nil (recordRedriveLedger would no-op anyway) and
// when the confirmation read itself errors, logs and skips the ledger write
// for every row -- the same "log, don't fail Handle" direction
// recordRedriveLedger's own per-row write failure already takes, since the
// edge write already committed and a confirmation-read hiccup must not cost
// a materialization retry/dead-letter.
func (h CrossplaneSatisfiedByMaterializationHandler) recordRedriveLedgerForConfirmedEdges(
	ctx context.Context,
	targetScopeID string,
	rows []map[string]any,
) {
	if h.RedriveTargetLedger == nil {
		return
	}
	confirmed, err := h.confirmWrittenCrossplaneSatisfiedByEdges(ctx, rows)
	if err != nil {
		slog.ErrorContext(
			ctx, "crossplane satisfied-by edge existence confirmation failed",
			log.ScopeID(targetScopeID),
			slog.String("error", err.Error()),
		)
		return
	}
	h.recordRedriveLedger(ctx, targetScopeID, confirmed)
}

// recordRedriveLedger records intent.ScopeID as confirmed satisfied for
// every DISTINCT (claim_group, claim_kind) identity among the rows this run
// actually wrote an edge for (issue #5476). Deliberately called AFTER
// WriteCrossplaneSatisfiedByEdges succeeds, never before and never at
// enqueue time: the ledger's meaning is "this target is satisfied for this
// XRD identity," which is only true once the edge is committed.
//
// A ledger-write failure is logged, not returned as a Handle() error: the
// edge write already committed (idempotent MERGE, safe to re-run), and
// failing the whole intent over a ledger-write hiccup would cost a
// materialization retry/dead-letter for no correctness gain. The safe
// direction is over-inclusion -- a future redrive sweep may redundantly
// re-enqueue this already-satisfied target once more -- never
// under-inclusion (the enqueue-time bug this design replaces, where a
// failure could permanently and silently suppress a real gap).
func (h CrossplaneSatisfiedByMaterializationHandler) recordRedriveLedger(
	ctx context.Context,
	targetScopeID string,
	rows []map[string]any,
) {
	if h.RedriveTargetLedger == nil {
		return
	}
	type identity struct{ group, kind string }
	seen := make(map[identity]struct{}, len(rows))
	for _, row := range rows {
		group := anyToString(row["claim_group"])
		kind := anyToString(row["claim_kind"])
		if group == "" || kind == "" {
			continue
		}
		key := identity{group: group, kind: kind}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if err := h.RedriveTargetLedger.RecordRedriven(ctx, targetScopeID, group, kind); err != nil {
			slog.ErrorContext(
				ctx, "crossplane redrive target ledger record failed",
				log.ScopeID(targetScopeID),
				slog.String("xrd_group", group),
				slog.String("xrd_claim_kind", kind),
				slog.String("error", err.Error()),
			)
		}
	}
}

// recordEdgeCounter emits the SATISFIED_BY edge-projection counter
// dimensioned by resolution_mode, mirroring
// KubernetesCorrelationMaterializationHandler.recordEdgeCounter.
func (h CrossplaneSatisfiedByMaterializationHandler) recordEdgeCounter(
	ctx context.Context,
	rows []map[string]any,
) {
	if h.Instruments == nil || h.Instruments.CrossplaneSatisfiedByEdges == nil {
		return
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[anyToString(row["resolution_mode"])]++
	}
	for mode, count := range counts {
		h.Instruments.CrossplaneSatisfiedByEdges.Add(ctx, int64(count), metric.WithAttributes(
			telemetry.AttrResolutionMode(mode),
		))
	}
}

type crossplaneSatisfiedByMaterializationTiming struct {
	intent           Intent
	factCount        int
	edgeCount        int
	ambiguousSkipped int
	skipRetract      bool
	loadDuration     time.Duration
	extractDuration  time.Duration
	retractDuration  time.Duration
	writeDuration    time.Duration
	totalDuration    time.Duration
}

func logCrossplaneSatisfiedByMaterializationCompleted(
	ctx context.Context,
	timing crossplaneSatisfiedByMaterializationTiming,
) {
	slog.InfoContext(
		ctx, "crossplane satisfied-by materialization completed",
		log.ScopeID(timing.intent.ScopeID),
		log.GenerationID(timing.intent.GenerationID),
		log.Domain(string(timing.intent.Domain)),
		slog.Int("fact_count", timing.factCount),
		slog.Int("edge_count", timing.edgeCount),
		slog.Int("ambiguous_skipped", timing.ambiguousSkipped),
		slog.Bool("skip_retract", timing.skipRetract),
		slog.Float64("load_facts_duration_seconds", timing.loadDuration.Seconds()),
		slog.Float64("classify_duration_seconds", timing.extractDuration.Seconds()),
		slog.Float64("retract_duration_seconds", timing.retractDuration.Seconds()),
		slog.Float64("graph_write_duration_seconds", timing.writeDuration.Seconds()),
		slog.Float64("total_duration_seconds", timing.totalDuration.Seconds()),
	)
}
