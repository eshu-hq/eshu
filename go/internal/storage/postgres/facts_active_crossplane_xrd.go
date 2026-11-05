// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// listActiveCrossplaneXRDFactsQuery loads active content_entity facts whose
// entity_type is CrossplaneXRD — the canonical Neo4j label string
// internal/content/shape/materialize.go's materializeEntities stamps onto
// every XRD content entity, not the lowercase key of
// internal/projector/canonical/materialization.go's entityTypeLabelMap — across every scope's
// currently active generation. XRDs commonly live in a separate platform repo
// from the Claims that reference them (issue #5347), so the Crossplane
// SATISFIED_BY correlation reducer joins across scopes exactly like
// listActiveContainerImageIdentityFactsQuery joins live Kubernetes workload
// evidence against cross-scope deployment-source image evidence.
const listActiveCrossplaneXRDFactsQuery = `
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'content_entity'
  AND fact.source_system = 'git'
  AND (
    fact.payload->>'entity_type' = 'CrossplaneXRD'
    OR fact.payload->>'entity_kind' = 'CrossplaneXRD'
  )
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
    $1::timestamptz IS NULL
    OR (fact.observed_at, fact.fact_id) > ($1::timestamptz, $2::text)
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $3
`

// listActiveCrossplaneClaimCandidateFactsQuery loads one scope's active
// K8sResource content_entity facts: the Claim candidates of the Crossplane
// SATISFIED_BY correlation (#10005). It reads the same rows the handler's
// own-scope content_entity load kept before, less the kinds
// ExtractCrossplaneSatisfiedByEdgeRows ignores and the same-scope XRDs, which
// listActiveCrossplaneXRDFactsQuery already returns for every scope.
const listActiveCrossplaneClaimCandidateFactsQuery = `
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.scope_id = $1
  AND fact.fact_kind = 'content_entity'
  AND fact.source_system = 'git'
  AND (
    fact.payload->>'entity_type' = 'K8sResource'
    OR fact.payload->>'entity_kind' = 'K8sResource'
  )
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
    $2::timestamptz IS NULL
    OR (fact.observed_at, fact.fact_id) > ($2::timestamptz, $3::text)
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $4
`

// Placeholders the derived base-candidate texts bind the delta-active scope
// ids at: the XRD read uses $1 to $3 for its keyset cursor and page size, the
// claim read $1 to $4 for its scope, cursor, and page size.
const (
	crossplaneXRDDeltaOverlayScopeParam   = 4
	crossplaneClaimDeltaOverlayScopeParam = 5
)

// crossplaneClaimQueryExecMode pins a custom plan on every page of the claim
// texts (#10005), the #9586 mechanism (see manifestDependencyQueryExecMode).
// Under the pool default each text is a named statement, and PostgreSQL
// switched the claim base text to its generic plan from the sixth execution:
// on the shim's 50,000-file scope it left fact_records_active_k8s_claim_redrive_idx
// for fact_records_scope_generation_keyset_idx, 275 ms against 16 ms custom.
// The cost is one replan per page. The XRD texts keep the pool default:
// their generic plan stays on fact_records_crossplane_xrd_entity_idx.
const crossplaneClaimQueryExecMode = pgx.QueryExecModeCacheDescribe

// ListActiveCrossplaneXRDFacts loads active CrossplaneXRD content_entity
// facts for the cross-scope Crossplane Claim -> XRD SATISFIED_BY correlation
// (issue #5347). Mirrors ListActiveContainerImageIdentityFacts's
// pagination shape.
//
// A scope whose active generation is a git delta reads its delta overlaid on
// its #7797 base full generation (#10003): an XRD in a file the delta did not
// touch stays visible, so a claim that resolves against it keeps its
// SATISFIED_BY edge. Every statement runs on one read-only repeatable-read
// snapshot; see overlayDeltaBase for the refused, legacy, and mismatched
// bases, which keep the delta-only rows.
func (s FactStore) ListActiveCrossplaneXRDFacts(ctx context.Context) ([]facts.Envelope, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	var loaded []facts.Envelope
	err := s.withDeltaOverlaySnapshot(ctx, func(snapshot *deltaOverlaySnapshot) error {
		var err error
		loaded, err = snapshot.listCrossplaneXRDOverlay(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return loaded, nil
}

// ListActiveCrossplaneSatisfiedByFacts loads everything one Crossplane
// SATISFIED_BY materialization of scopeID resolves: the scope's active Claim
// candidates (K8sResource content entities) and every scope's active
// CrossplaneXRD content entities (#10005). Both legs read one read-only
// repeatable-read snapshot and share one delta-target lookup, so they see the
// same active generations. A delta-active scope reads its delta overlaid on
// its #7797 base full generation in both legs: an unchanged claim beside a
// changed one, and an XRD in an untouched file, stay visible, so the handler's
// retract-by-scope and rewrite keeps their edges. With no delta-active scope
// the call adds only the target lookup to the two active reads.
// Performance Evidence: docs/internal/evidence/10005-crossplane-delta-overlay.md.
func (s FactStore) ListActiveCrossplaneSatisfiedByFacts(ctx context.Context, scopeID string) ([]facts.Envelope, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	var loaded []facts.Envelope
	err := s.withDeltaOverlaySnapshot(ctx, func(snapshot *deltaOverlaySnapshot) error {
		claims, err := snapshot.listCrossplaneClaimOverlay(ctx, scopeID)
		if err != nil {
			return err
		}
		xrds, err := snapshot.listCrossplaneXRDOverlay(ctx)
		if err != nil {
			return err
		}
		loaded = append(claims, xrds...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return loaded, nil
}

// listCrossplaneXRDOverlay reads every scope's active XRDs plus, for each
// delta-active scope with a baseline, the XRDs of its base the delta did not
// override, delete, or touch.
func (snapshot *deltaOverlaySnapshot) listCrossplaneXRDOverlay(ctx context.Context) ([]facts.Envelope, error) {
	reader := snapshot.store
	sink := reader.crossplaneDeltaOverlayTelemetry(crossplaneSatisfiedByReadXRD)
	baseQuery, err := deriveDeltaOverlayBaseQuery(listActiveCrossplaneXRDFactsQuery, crossplaneXRDDeltaOverlayScopeParam)
	if err != nil {
		return nil, err
	}
	active, err := reader.listCrossplaneFactPages(ctx, "list active crossplane xrd facts", listActiveCrossplaneXRDFactsQuery, nil, nil)
	if err != nil {
		return nil, err
	}
	targets, err := snapshot.deltaTargets(ctx)
	if err != nil {
		reader.recordDeltaOverlayFailure(ctx, sink)
		return nil, err
	}
	var base []facts.Envelope
	if baseScopes := deltaOverlayBaseScopes(targets); len(baseScopes) > 0 {
		base, err = reader.listCrossplaneFactPages(ctx, "list delta base crossplane xrd facts", baseQuery, nil, nil, baseScopes)
		if err != nil {
			reader.recordDeltaOverlayFailure(ctx, sink)
			return nil, err
		}
	}
	return reader.overlayDeltaBase(ctx, sink, targets, active, base)
}

// listCrossplaneClaimOverlay reads scopeID's active Claim candidates plus,
// when scopeID itself is delta-active with a baseline, the candidates of its
// base the delta did not override, delete, or touch. No base statement runs
// for a scope whose active generation is a full generation.
func (snapshot *deltaOverlaySnapshot) listCrossplaneClaimOverlay(ctx context.Context, scopeID string) ([]facts.Envelope, error) {
	reader := snapshot.store
	sink := reader.crossplaneDeltaOverlayTelemetry(crossplaneSatisfiedByReadClaim)
	baseQuery, err := deriveDeltaOverlayBaseQuery(listActiveCrossplaneClaimCandidateFactsQuery, crossplaneClaimDeltaOverlayScopeParam)
	if err != nil {
		return nil, err
	}
	execMode := []any{crossplaneClaimQueryExecMode}
	scopeArgs := []any{scopeID}
	active, err := reader.listCrossplaneFactPages(ctx, "list active crossplane claim facts", listActiveCrossplaneClaimCandidateFactsQuery, execMode, scopeArgs)
	if err != nil {
		return nil, err
	}
	targets, err := snapshot.deltaTargets(ctx)
	if err != nil {
		reader.recordDeltaOverlayFailure(ctx, sink)
		return nil, err
	}
	targets = slices.DeleteFunc(slices.Clone(targets), func(target deltaOverlayTarget) bool {
		return target.scopeID != scopeID
	})
	var base []facts.Envelope
	if baseScopes := deltaOverlayBaseScopes(targets); len(baseScopes) > 0 {
		base, err = reader.listCrossplaneFactPages(ctx, "list delta base crossplane claim facts", baseQuery, execMode, scopeArgs, baseScopes)
		if err != nil {
			reader.recordDeltaOverlayFailure(ctx, sink)
			return nil, err
		}
	}
	return reader.overlayDeltaBase(ctx, sink, targets, active, base)
}

// listCrossplaneFactPages runs one Crossplane reader text through every
// keyset page. mode, when set, is the pgx exec mode argument; lead binds
// before the cursor and page size; extra binds after them, on every page.
func (s FactStore) listCrossplaneFactPages(
	ctx context.Context,
	label string,
	query string,
	mode []any,
	lead []any,
	extra ...any,
) ([]facts.Envelope, error) {
	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listCrossplaneFactsPage(ctx, label, query, slices.Concat(mode, lead), cursorObservedAt, cursorFactID, extra)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, page...)
		if len(page) < listFactsByKindPageSize {
			return loaded, nil
		}

		last := page[len(page)-1]
		observedAt := last.ObservedAt.UTC()
		cursorObservedAt = &observedAt
		cursorFactID = last.FactID
	}
}

func (s FactStore) listCrossplaneFactsPage(
	ctx context.Context,
	label string,
	query string,
	lead []any,
	cursorObservedAt *time.Time,
	cursorFactID string,
	extra []any,
) ([]facts.Envelope, error) {
	var cursor any
	if cursorObservedAt != nil {
		cursor = cursorObservedAt.UTC()
	}

	args := slices.Concat(lead, []any{cursor, cursorFactID, listFactsByKindPageSize}, extra)
	rows, err := s.database.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := backfill.ScanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("%s: %w", label, scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}

	return loaded, nil
}

// Read label values of eshu_dp_crossplane_satisfied_by_read_delta_union_total:
// which Crossplane SATISFIED_BY leg made the overlay decision.
const (
	crossplaneSatisfiedByReadXRD   = "xrd"
	crossplaneSatisfiedByReadClaim = "claim"
)

// crossplaneDeltaOverlayTelemetry names where a Crossplane SATISFIED_BY read
// records its delta-overlay decisions:
// eshu_dp_crossplane_satisfied_by_read_delta_union_total by read and outcome
// (#10003, #10005), the crossplane_satisfied_by_read.delta_union span event,
// and WARN lines starting "crossplane satisfied-by read".
func (s FactStore) crossplaneDeltaOverlayTelemetry(read string) deltaOverlayTelemetry {
	sink := deltaOverlayTelemetry{
		read:      read,
		subject:   "crossplane satisfied-by read",
		spanEvent: "crossplane_satisfied_by_read.delta_union",
	}
	if s.instruments != nil {
		sink.counter = s.instruments.CrossplaneSatisfiedByReadDeltaUnion
	}
	return sink
}
