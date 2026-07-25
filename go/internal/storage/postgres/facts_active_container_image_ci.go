// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// listActiveContainerImageCIFactsFilterSQL is the predicate shared by the load
// query below and migrations/081_fact_records_active_container_image_ci_idx.sql
// (fact_records_active_container_image_ci_idx). It admits two arms:
//
//   - ci.run: every run row is a potential repository/commit anchor
//     (containerImageCIRuns, go/internal/reducer/container_image_identity_typed_evidence.go),
//     so it cannot be narrowed further.
//   - ci.artifact: narrowed to artifact_type = 'container_image'. Non-image
//     artifacts (coverage reports, SBOM bundles, test reports) are the bulk of
//     ci.artifact volume in practice, and the reducer already ignores them
//     (addCICDArtifactImageReference returns early for a non-container_image
//     artifact_type -- see container_image_identity_typed_evidence.go). Admitting
//     them here would inflate this cross-scope load with rows the reducer
//     discards on every refresh.
//
// This predicate MUST stay identical to the migration's WHERE clause --
// TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL locks the
// two together the same way
// TestIdentityEpochIndexPredicateMatchesIdentityFactFilter locks
// identityFactFilterSQL to migration 077 (identity_epoch_cache_contract_test.go).
const listActiveContainerImageCIFactsFilterSQL = `(
    (fact.fact_kind = 'ci.run' AND fact.source_system = 'ci_cd_run')
    OR (fact.fact_kind = 'ci.artifact' AND fact.source_system = 'ci_cd_run'
        AND fact.payload->>'artifact_type' = 'container_image')
  )`

const listActiveContainerImageCIFactsQuery = `
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
WHERE ` + listActiveContainerImageCIFactsFilterSQL + `
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
    $1::timestamptz IS NULL
    OR (fact.observed_at, fact.fact_id) > ($1::timestamptz, $2::text)
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $3
`

// ListActiveContainerImageCIFacts loads active ci.run and (container-image-typed)
// ci.artifact facts across CI-run-owned scopes (issue #5810, dedicated
// cross-scope loader following the #5456 PR #5707 P1-b SLSA precedent instead
// of widening identityFactFilterSQL). The container_image_identity reducer's
// DERIVED_FROM (base-image lineage, #5460) projection is owner-scoped to the
// repository whose Dockerfile declares the base image
// (projectContainerImageDerivedFromEdges,
// go/internal/reducer/container_image_derived_from_edges.go); the CI provider's
// run->artifact->digest evidence that proves a repository BUILT a given digest
// is written by the ci_cd_run collector in the CI run's OWN scope, a different
// scope than the repository the DERIVED_FROM projection actually runs in. Without
// this cross-scope bridge, a repository-scoped refresh can never see CI build
// provenance for a digest evidenced only in another scope's generation, so
// BuildProvenanceRepositoryIDs (container_image_identity.go) could never reach a
// value at the repository-owned projection and the CI tier of DERIVED_FROM could
// never durably materialize outside a same-scope unit test (#5810).
//
// A dedicated loader was chosen over widening identityFactFilterSQL/
// fact_records_identity_epoch_idx deliberately: ci.run/ci.artifact is the
// highest-churn fact family in the system (one row per CI run and per build
// artifact), and identityFactFilterSQL backs the drift-locked identity-epoch
// cache contract (identity_epoch_cache.go, identity_epoch_cache_contract_test.go)
// that fact_records_active_container_image_slsa.go's doc comment explicitly
// keeps SLSA/verification facts separate from for the same reason. Coupling
// CI-run churn into that cache's probe query would move its count/max_observed_at/
// fingerprint on every CI run, defeating the cache far more often than the
// identity-fact family it was built for.
//
// Mirrors ListActiveContainerImageSLSAFacts (facts_active_container_image_slsa.go)
// and ListActiveRepositoryFacts (facts_active_repository.go): a simple paginated
// cross-scope loader with its own dedicated partial index
// (fact_records_active_container_image_ci_idx, migrations/079), no epoch-cache/
// fingerprint machinery.
//
// The reducer's Handle path (container_image_identity.go) MUST dedupe this
// loader's envelopes against its own scope-local load by FactID before decoding:
// a CI-scope intent's own scope-local ci.run/ci.artifact facts overlap this
// cross-scope load for the SAME envelopes once that CI scope's generation is
// active, and a malformed fact would otherwise be quarantined and counted twice.
//
// Evidence decay is expected and already true of BUILT_FROM's CI tier: a run
// that ages out of the bounded run window (runwatermark.Watermark, see
// go/internal/collector/cicdrun/runwatermark/types.go) is retracted like any
// other fact once no longer active, and the DERIVED_FROM edge it anchored
// retracts on the next refresh of the owning repository's generation
// (retract-first-per-generation, projectContainerImageDerivedFromEdges).
func (s FactStore) ListActiveContainerImageCIFacts(ctx context.Context) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}

	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listActiveContainerImageCIFactsPage(ctx, cursorObservedAt, cursorFactID)
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

func (s FactStore) listActiveContainerImageCIFactsPage(
	ctx context.Context,
	cursorObservedAt *time.Time,
	cursorFactID string,
) ([]facts.Envelope, error) {
	var cursor any
	if cursorObservedAt != nil {
		cursor = cursorObservedAt.UTC()
	}

	rows, err := s.db.QueryContext(
		ctx,
		listActiveContainerImageCIFactsQuery,
		cursor,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list active container image CI facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active container image CI facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active container image CI facts: %w", err)
	}

	return loaded, nil
}
