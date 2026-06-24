// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// listTerraformBackendCanonicalRowsQuery returns one row per terraform_backends
// fact emitted into a sealed repo_snapshot generation. The adapter decodes the
// JSON array in Go, converts each entry into a terraformstate.StateKey via the
// shared parser-fact helper, and filters in-memory by the requested
// (backend_kind, locator_hash) pair.
//
// The SQL deliberately does NOT filter by repo_id; the resolver call site does
// not know the owning repo yet — discovering it is the whole point.
//
// The HCL parser's base payload (parser.go:110) always emits an empty
// terraform_backends array for every parsed file, so jsonb_typeof alone does
// NOT prune the scan. The array-length filter is the load-bearing predicate:
// it restricts the row set to files that actually contain a
// `terraform { backend "<kind>" {} }` block — typically one or two files per
// Terraform repo, often zero. Without it the adapter decodes every HCL file
// fact across every active repo.
//
// The CASE expression provides both the type guard and the length filter
// in one predicate. Postgres does NOT guarantee short-circuit evaluation
// of AND predicates — the planner can evaluate jsonb_array_length before
// any standalone jsonb_typeof guard, raising "cannot get array length of
// a scalar" (SQLSTATE 22023) on rows whose path value is jsonb null or
// any other scalar. CASE branch evaluation IS guaranteed not to evaluate
// non-matching arms (PostgreSQL docs: "expressions in a CASE expression
// are not evaluated unless required"), so the CASE alone safely emits 0
// for non-array values and the real length for arrays. Adding a separate
// `jsonb_typeof = 'array'` predicate next to this CASE would be redundant
// and would re-evaluate the type check per row. Regression test:
// TestPostgresTerraformBackendQuerySurvivesNullTerraformBackendsPath.
const listTerraformBackendCanonicalRowsQuery = `
WITH backend_generations AS (
    SELECT DISTINCT
        fact.payload->>'repo_id' AS repo_id,
        fact.scope_id,
        fact.generation_id
    FROM fact_records AS fact
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact.scope_id
     AND scope.active_generation_id = fact.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact.scope_id
     AND generation.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'file'
      AND fact.source_system = 'git'
      AND generation.status = 'active'
      AND CASE
            WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_backends') = 'array'
            THEN jsonb_array_length(fact.payload->'parsed_file_data'->'terraform_backends')
            ELSE 0
          END > 0
)
SELECT
    backend.repo_id                                           AS repo_id,
    fact.scope_id                                             AS scope_id,
    fact.generation_id                                        AS generation_id,
    fact.observed_at                                          AS observed_at,
    jsonb_build_object(
        'terraform_backends', COALESCE(fact.payload->'parsed_file_data'->'terraform_backends', '[]'::jsonb),
        'terraform_variables', COALESCE(fact.payload->'parsed_file_data'->'terraform_variables', '[]'::jsonb),
        'terraform_locals', COALESCE(fact.payload->'parsed_file_data'->'terraform_locals', '[]'::jsonb)
    )                                                         AS terraform_backend_context
FROM backend_generations AS backend
JOIN fact_records AS fact
  ON fact.scope_id = backend.scope_id
 AND fact.generation_id = backend.generation_id
 AND fact.payload->>'repo_id' = backend.repo_id
WHERE fact.fact_kind = 'file'
  AND fact.source_system = 'git'
  AND (
      CASE
        WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_backends') = 'array'
        THEN jsonb_array_length(fact.payload->'parsed_file_data'->'terraform_backends')
        ELSE 0
      END
    + CASE
        WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_variables') = 'array'
        THEN jsonb_array_length(fact.payload->'parsed_file_data'->'terraform_variables')
        ELSE 0
      END
    + CASE
        WHEN jsonb_typeof(fact.payload->'parsed_file_data'->'terraform_locals') = 'array'
        THEN jsonb_array_length(fact.payload->'parsed_file_data'->'terraform_locals')
        ELSE 0
      END
  ) > 0
ORDER BY backend.repo_id ASC, fact.scope_id ASC, fact.generation_id ASC, fact.observed_at ASC, fact.fact_id ASC
`

// PostgresTerraformBackendQuery answers
// tfstatebackend.TerraformBackendQuery from durable parser facts. The adapter
// reads terraform_backends rows out of fact_records, recomputes each row's
// safe locator hash with terraformstate.ScopeLocatorHash, and returns every
// row whose composite (backend_kind, locator_hash) matches the caller. The
// adapter never deduplicates owners: the resolver depends on seeing every
// matching repo so it can return ErrAmbiguousBackendOwner when more than one
// claims the same composite key.
//
// The hash MUST be the version-agnostic ScopeLocatorHash, not the per-version
// LocatorHash. The caller-supplied locatorHash is parsed out of a
// state-snapshot scope ID built by scope.NewTerraformStateSnapshotScope,
// which is intentionally version-agnostic; using LocatorHash here would
// silently reject every drift candidate (issue #203).
type PostgresTerraformBackendQuery struct {
	DB Queryer
}

// ListTerraformBackendsByLocator returns every sealed config-side
// terraform_backend row whose (backend_kind, locator_hash) matches the input.
// The locator hash on each row mirrors terraformstate.ScopeLocatorHash
// applied to the parser-side backend block (BackendKind + Locator only,
// version-agnostic). The state side hashes the locator the same way when
// scope.NewTerraformStateSnapshotScope builds the durable scope ID, so the
// join is hash-stable across config and state sources.
//
// The method returns:
//
//   - ([], nil) when no row matches (let the resolver translate to
//     ErrNoConfigRepoOwnsBackend).
//   - All matching rows including duplicates across repos (let the resolver
//     translate to ErrAmbiguousBackendOwner when len(unique RepoID) > 1).
//
// Blank inputs are rejected as errors; the resolver already trims and
// validates before calling, but the adapter enforces the same contract to
// keep accidental empty scans out of fact_records.
func (q PostgresTerraformBackendQuery) ListTerraformBackendsByLocator(
	ctx context.Context,
	backendKind string,
	locatorHash string,
) ([]tfstatebackend.TerraformBackendRow, error) {
	if q.DB == nil {
		return nil, fmt.Errorf("terraform backend canonical database is required")
	}
	backendKind = strings.TrimSpace(backendKind)
	if backendKind == "" {
		return nil, fmt.Errorf("backend kind must not be blank")
	}
	locatorHash = strings.TrimSpace(locatorHash)
	if locatorHash == "" {
		return nil, fmt.Errorf("locator hash must not be blank")
	}

	rows, err := q.DB.QueryContext(ctx, listTerraformBackendCanonicalRowsQuery)
	if err != nil {
		return nil, fmt.Errorf("list terraform backend canonical rows: %w", err)
	}
	defer func() { _ = rows.Close() }()

	contexts := map[terraformBackendCanonicalContextKey]*terraformBackendCanonicalContext{}
	order := make([]terraformBackendCanonicalContextKey, 0)
	for rows.Next() {
		var repoID string
		var scopeID string
		var generationID string
		var observedAt time.Time
		var rawContext []byte
		if err := rows.Scan(&repoID, &scopeID, &generationID, &observedAt, &rawContext); err != nil {
			return nil, fmt.Errorf("scan terraform backend canonical row: %w", err)
		}

		contextValue, err := decodeTerraformBackendFactContext(rawContext)
		if err != nil {
			return nil, err
		}
		key := terraformBackendCanonicalContextKey{
			repoID:       strings.TrimSpace(repoID),
			scopeID:      strings.TrimSpace(scopeID),
			generationID: strings.TrimSpace(generationID),
		}
		if key.repoID == "" || key.scopeID == "" || key.generationID == "" {
			continue
		}
		group, seen := contexts[key]
		if !seen {
			group = &terraformBackendCanonicalContext{
				repoID:       key.repoID,
				scopeID:      key.scopeID,
				generationID: key.generationID,
				observedAt:   observedAt.UTC(),
			}
			contexts[key] = group
			order = append(order, key)
		}
		group.context = mergeTerraformBackendFactContext(group.context, contextValue)
		if observedAt.Before(group.observedAt) {
			group.observedAt = observedAt.UTC()
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate terraform backend canonical rows: %w", err)
	}

	var out []tfstatebackend.TerraformBackendRow
	for _, key := range order {
		group := contexts[key]
		matches := matchingBackendRowsFromContext(
			group.repoID, group.scopeID, group.generationID, group.observedAt, group.context, backendKind, locatorHash,
		)
		out = append(out, matches...)
	}
	return out, nil
}

type terraformBackendCanonicalContextKey struct {
	repoID       string
	scopeID      string
	generationID string
}

type terraformBackendCanonicalContext struct {
	repoID       string
	scopeID      string
	generationID string
	observedAt   time.Time
	context      terraformBackendFactContext
}

// matchingBackendRowsFromContext converts one active repo-generation context
// into canonical backend rows. Entries that fail the exact-attribute filter are
// silently skipped because drift detection requires deterministic locator
// hashes and cannot operate on ambiguous inputs.
func matchingBackendRowsFromContext(
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	contextValue terraformBackendFactContext,
	backendKind string,
	locatorHash string,
) []tfstatebackend.TerraformBackendRow {
	candidates := terraformBackendCandidatesFromContext(repoID, contextValue)
	return matchingBackendRowsFromCandidates(repoID, scopeID, generationID, observedAt, candidates, backendKind, locatorHash)
}

func matchingBackendRowsFromCandidates(
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	candidates []terraformstate.DiscoveryCandidate,
	backendKind string,
	locatorHash string,
) []tfstatebackend.TerraformBackendRow {
	var out []tfstatebackend.TerraformBackendRow
	for _, candidate := range candidates {
		gotKind := strings.TrimSpace(string(candidate.State.BackendKind))
		if gotKind != backendKind {
			continue
		}
		// Use ScopeLocatorHash, NOT LocatorHash, for the canonical join key.
		// The state side (drift handler) parses this hash out of the
		// state-snapshot scope ID, which is built by
		// scope.NewTerraformStateSnapshotScope and is intentionally
		// version-agnostic. LocatorHash digests VersionID and would diverge
		// here for empty VersionID by exactly one trailing null byte,
		// silently rejecting every drift candidate (issue #203).
		gotHash := terraformstate.ScopeLocatorHash(candidate.State.BackendKind, candidate.State.Locator)
		if gotHash != locatorHash {
			continue
		}
		out = append(out, tfstatebackend.TerraformBackendRow{
			RepoID:           strings.TrimSpace(repoID),
			ScopeID:          strings.TrimSpace(scopeID),
			CommitID:         strings.TrimSpace(generationID),
			CommitObservedAt: observedAt.UTC(),
			BackendKind:      gotKind,
			LocatorHash:      gotHash,
		})
	}
	return out
}
