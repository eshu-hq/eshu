// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// listActiveContainerImageCIRunArmSQL and listActiveContainerImageCIArtifactArmSQL
// are the two fact-kind arms migrations/081_fact_records_active_container_image_ci_idx.sql
// (fact_records_active_container_image_ci_idx) indexes together:
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
// listActiveContainerImageCIFactsFilterSQL composes the two arms back into
// migration 081's exact two-arm shape purely so
// TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL can keep
// locking that migration's WHERE clause to a Go constant -- the live query
// below (#5810 P1 follow-up) no longer issues that combined predicate as a
// single scan (see the doc comment on listActiveContainerImageCIFactsQuery
// for why), but migration 081's index remains valid, already-shipped, and
// harmless to keep: an owner-scoped repository refresh with a small owned-run
// count no longer needs it, but a caller that ever needs the full
// unfiltered cross-scope family back (there is none today) still has a
// working index to land on.
const (
	listActiveContainerImageCIRunArmSQL      = `fact.fact_kind = 'ci.run' AND fact.source_system = 'ci_cd_run'`
	listActiveContainerImageCIArtifactArmSQL = `fact.fact_kind = 'ci.artifact' AND fact.source_system = 'ci_cd_run'
        AND fact.payload->>'artifact_type' = 'container_image'`
)

const listActiveContainerImageCIFactsFilterSQL = `(
    (` + listActiveContainerImageCIRunArmSQL + `)
    OR (` + listActiveContainerImageCIArtifactArmSQL + `)
  )`

// listActiveContainerImageCIRunRepositoryFilterSQL is the predicate shared by
// the owner-scoped ci.run branch below and
// migrations/082_fact_records_active_container_image_ci_run_repository_idx.sql
// (fact_records_active_container_image_ci_run_repository_idx). Unlike
// listActiveContainerImageCIFactsFilterSQL (a residual filter applied to
// whatever rows the index scan already visits), this predicate covers
// is_tombstone too: migration 082 exists to serve a single point lookup by
// repository_id, so folding the tombstone check into the index keeps a
// retracted run's stale claim out of it entirely.
//
// This predicate MUST stay identical to the migration's WHERE clause --
// TestFactRecordsActiveContainerImageCIRunRepositoryIndexPredicateMatchesFilterSQL
// locks the two together, mirroring
// TestFactRecordsActiveContainerImageCIIndexPredicateMatchesFilterSQL for
// migration 081.
const listActiveContainerImageCIRunRepositoryFilterSQL = `fact_kind = 'ci.run'
      AND source_system = 'ci_cd_run'
      AND is_tombstone = FALSE`

// listActiveContainerImageCIRunRepositoryFilterSQLQualified is
// listActiveContainerImageCIRunRepositoryFilterSQL with every column
// table-qualified for use inside listActiveContainerImageCIFactsQuery's
// multi-table joins: ingestion_scopes also carries its own source_system
// column, so the unqualified migration-predicate text
// (listActiveContainerImageCIRunRepositoryFilterSQL, correct for a
// single-table CREATE INDEX WHERE clause) is ambiguous once fact_records is
// joined to ingestion_scopes/scope_generations in the live query.
const listActiveContainerImageCIRunRepositoryFilterSQLQualified = `fact.fact_kind = 'ci.run'
      AND fact.source_system = 'ci_cd_run'
      AND fact.is_tombstone = FALSE`

// listActiveContainerImageCIFactsQuery pushes the calling intent's owning
// repository down into Postgres (#5810 P1 follow-up) instead of loading
// every active ci.run/ci.artifact fact platform-wide and filtering to the
// owner in Go (filterContainerImageCIFactsForOwner,
// go/internal/reducer/container_image_identity_ci_loader.go). At the
// documented 500,000-active-CI-run-scope worst case (see
// docs/internal/evidence/5810-cross-scope-ci-loader.md), the prior
// unfiltered shape still transferred and decoded every active CI fact on
// every repository-scoped refresh only to discard nearly all of it in Go --
// migration 081's index bounded the SCAN to the ci.run/ci.artifact family,
// but never bounded the RESULT SIZE to one repository's evidence.
//
// The query is a two-branch UNION ALL rather than a single WHERE with an
// extra owner AND-clause: the planner cannot use migration 082's
// repository_id-keyed index to drive the whole query when the ci.run and
// ci.artifact arms stay combined in one scan (a materialized owned_runs CTE
// joined back in via a correlated condition still forced a nested loop over
// every active scope in this session's prove-the-theory measurement -- see
// the evidence doc's rejected-candidate row). Splitting into two independent
// branches lets each pick its own best access path: the ci.run branch drives
// straight off the new repository_id index, and the ci.artifact branch joins
// the (tiny, already-materialized) owned_runs result back by scope_id and
// the provider/run_id/run_attempt tuple, landing on the existing
// fact_records_scope_generation_idx.
//
// owned_runs is declared MATERIALIZED (not the PostgreSQL 12+ default
// inlining behavior for a once-referenced CTE) because the ci.artifact
// branch's join predicate is NOT a simple equality PostgreSQL could push
// down through an inlined reference -- forcing materialization guarantees
// the owner's run set is computed exactly once and then hash-joined, not
// re-evaluated per candidate row.
const listActiveContainerImageCIFactsQuery = `
WITH owned_runs AS MATERIALIZED (
    SELECT
        fact.scope_id,
        fact.payload->>'provider' AS provider,
        fact.payload->>'run_id' AS run_id,
        COALESCE(NULLIF(fact.payload->>'run_attempt', ''), '1') AS run_attempt
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE ` + listActiveContainerImageCIRunRepositoryFilterSQLQualified + `
      AND generation.status = 'active'
      AND fact.payload->>'repository_id' = $1
),
candidates AS (
    SELECT
        fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind,
        fact.stable_fact_key, fact.schema_version, fact.collector_kind,
        fact.fencing_token, fact.source_confidence, fact.source_system,
        fact.source_fact_key, fact.source_uri, fact.source_record_id,
        fact.observed_at, fact.is_tombstone, fact.payload
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE ` + listActiveContainerImageCIRunRepositoryFilterSQLQualified + `
      AND generation.status = 'active'
      AND fact.payload->>'repository_id' = $1

    UNION ALL

    SELECT
        fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind,
        fact.stable_fact_key, fact.schema_version, fact.collector_kind,
        fact.fencing_token, fact.source_confidence, fact.source_system,
        fact.source_fact_key, fact.source_uri, fact.source_record_id,
        fact.observed_at, fact.is_tombstone, fact.payload
    FROM fact_records AS fact
    JOIN owned_runs
      ON owned_runs.scope_id = fact.scope_id
     AND owned_runs.provider = fact.payload->>'provider'
     AND owned_runs.run_id = fact.payload->>'run_id'
     AND owned_runs.run_attempt = COALESCE(NULLIF(fact.payload->>'run_attempt', ''), '1')
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE ` + listActiveContainerImageCIArtifactArmSQL + `
      AND fact.is_tombstone = FALSE
      AND generation.status = 'active'
)
SELECT
    fact_id,
    scope_id,
    generation_id,
    fact_kind,
    stable_fact_key,
    schema_version,
    collector_kind,
    fencing_token,
    source_confidence,
    source_system,
    source_fact_key,
    COALESCE(source_uri, ''),
    COALESCE(source_record_id, ''),
    observed_at,
    is_tombstone,
    payload
FROM candidates
WHERE (
    $2::timestamptz IS NULL
    OR (observed_at, fact_id) > ($2::timestamptz, $3::text)
  )
ORDER BY observed_at ASC, fact_id ASC
LIMIT $4
`

// ListActiveContainerImageCIFacts loads active ci.run and (container-image-typed)
// ci.artifact facts owned by ownerRepositoryID (issue #5810, dedicated
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
// ownerRepositoryID is required (a blank value returns immediately with no
// query issued): the caller, ContainerImageIdentityHandler.loadActiveContainerImageCIFacts
// (go/internal/reducer/container_image_identity_ci_loader.go), already skips
// calling this loader at all for a non-repository scope, so a blank owner
// reaching here would only ever be a caller bug -- returning an empty result
// rather than issuing an unbounded, unowned query is the safe failure mode.
//
// The reducer's Handle path (container_image_identity.go) still runs
// filterContainerImageCIFactsForOwner over this loader's result as a
// defense-in-depth correctness net: this SQL predicate is the performance
// bound, not the sole correctness guarantee, so a future FactLoader
// implementation that does not itself filter correctly still cannot leak
// foreign build provenance into a repository-scoped intent.
//
// Evidence decay is expected and already true of BUILT_FROM's CI tier: a run
// that ages out of the bounded run window (runwatermark.Watermark, see
// go/internal/collector/cicdrun/runwatermark/types.go) is retracted like any
// other fact once no longer active, and the DERIVED_FROM edge it anchored
// retracts on the next refresh of the owning repository's generation
// (retract-first-per-generation, projectContainerImageDerivedFromEdges).
func (s FactStore) ListActiveContainerImageCIFacts(ctx context.Context, ownerRepositoryID string) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	if ownerRepositoryID == "" {
		return nil, nil
	}

	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listActiveContainerImageCIFactsPage(ctx, ownerRepositoryID, cursorObservedAt, cursorFactID)
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
	ownerRepositoryID string,
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
		ownerRepositoryID,
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
