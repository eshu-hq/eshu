// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// canonicalGoRepositoryIDPattern accepts exactly the canonical repository ids
// a go-reachability scope can name, mirroring scannercomponent's pattern
// (scannercomponent.GoReachabilityScopeID owns it): "repository:r_" plus 8
// lowercase hex digits. Anything else (a URL, a local path, a git-scope
// alias) is refused before it can reach a scope id. The scope prefix is the
// existing goReachabilityScopePrefix, so the fence, the purge, and this
// reader build the same scope id.
var canonicalGoRepositoryIDPattern = regexp.MustCompile(`^repository:r_[0-9a-f]{8}$`)

// goReachabilityEvidenceKinds returns the two govulncheck evidence kinds the
// cross-scope Go stage loads, in chain-package spelling. The probe binds them
// as the fact_kind arm of the (scope_id, generation_id, fact_kind) probe.
func goReachabilityEvidenceKinds() []string {
	return []string{
		chain.VulnerabilityGoModuleEvidenceFactKind,
		chain.VulnerabilityGoCallReachabilityFactKind,
	}
}

// goReachabilityScopeIDs maps repository ids to their go-reachability scope
// ids, dropping blanks, non-canonical ids, and duplicates and sorting the
// survivors so one filter always binds one scope list.
func goReachabilityScopeIDs(repositoryIDs []string) []string {
	seen := make(map[string]struct{}, len(repositoryIDs))
	scopes := make([]string, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		repositoryID = strings.TrimSpace(repositoryID)
		if !canonicalGoRepositoryIDPattern.MatchString(repositoryID) {
			continue
		}
		scopeID := goReachabilityScopePrefix + repositoryID
		if _, ok := seen[scopeID]; ok {
			continue
		}
		seen[scopeID] = struct{}{}
		scopes = append(scopes, scopeID)
	}
	slices.Sort(scopes)
	return scopes
}

// goReachabilityScopeResolutionSQL resolves the active generation of each
// go-reachability scope the filter names. Scopes without an active generation
// contribute no rows, so a repository the scanner never addressed (or whose
// scope has no active generation) simply loads nothing.
const goReachabilityScopeResolutionSQL = `SELECT scope.scope_id, scope.active_generation_id
FROM ingestion_scopes AS scope
WHERE scope.scope_id = ANY($1::text[])
  AND scope.active_generation_id IS NOT NULL
ORDER BY scope.scope_id`

// listActiveGoReachabilityFactsQuery probes one (scope_id, generation_id,
// fact_kind) triple per candidate on fact_records_scope_generation_idx, in the
// same shape as the facts/impact repository-scope probe: the three bound
// arrays are all Index Cond, and the active-generation join re-checks the
// resolved pair. No new index: the existing index leads on exactly these
// columns. Core evidence is never bounded, so there is no LIMIT.
const listActiveGoReachabilityFactsQuery = `
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
WHERE fact.scope_id = ANY($1::text[])
  AND fact.generation_id = ANY($2::text[])
  AND fact.fact_kind = ANY($3::text[])
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
`

// ListActiveGoReachabilityFacts loads the active generation's govulncheck
// evidence (vulnerability.go_module_evidence and
// vulnerability.go_call_reachability) for every canonical repository id in
// the filter, from that repository's go-reachability scope. It is the
// production loader behind the supply-chain-impact cross-scope Go stage
// (activeGoReachabilityFactLoader): before it, the stage no-ops and findings
// without joined Go evidence read module_only or unknown.
//
// A scope whose active generation holds no Go evidence, a repository id that
// cannot name a go-reachability scope, and an empty filter all load nothing
// (nil, nil): the stage then contributes no envelopes, exactly as before.
// Tombstoned rows are never surfaced as live: the active-generation pointer
// keeps tombstoned rows joinable, so the predicate is the only guard against
// returning superseded Go evidence to the classifier. Go-reachability scopes
// are scanner evidence scopes, never git deltas, so the read is
// single-generation like the other kind-filtered active readers.
//
// The probe needs no new index and no LIMIT. Every statement runs on one
// read-only repeatable-read snapshot, so the resolved generations and the
// fact rows see one state.
//
// No-Observability-Change: the pass-level go_reachability_facts count and
// load_go_reachability sub-duration the stage already emits cover this read;
// no store-level metric is added.
func (s FactStore) ListActiveGoReachabilityFacts(
	ctx context.Context,
	filter supplychaincore.GoReachabilityFactFilter,
) ([]facts.Envelope, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	scopeIDs := goReachabilityScopeIDs(filter.RepositoryIDs)
	if len(scopeIDs) == 0 {
		return nil, nil
	}
	query, err := s.protectedImpactStaticFactSQL(listActiveGoReachabilityFactsQuery)
	if err != nil {
		return nil, err
	}
	var loaded []facts.Envelope
	err = withReadOnlyRepeatableRead(ctx, s.database, func(queryer db.Queryer) error {
		scopes, generations, err := resolveGoReachabilityScopes(ctx, queryer, scopeIDs)
		if err != nil {
			return err
		}
		if len(scopes) == 0 {
			return nil
		}
		loaded, err = listGoReachabilityFactRows(ctx, queryer, query, scopes, generations)
		return err
	})
	if err != nil {
		return nil, err
	}
	return loaded, nil
}

// resolveGoReachabilityScopes returns the active (scope, generation) pairs
// for the go-reachability scopes the filter named; an empty list issues no
// statement.
func resolveGoReachabilityScopes(
	ctx context.Context,
	queryer db.Queryer,
	scopeIDs []string,
) ([]string, []string, error) {
	rows, err := queryer.QueryContext(ctx, goReachabilityScopeResolutionSQL, scopeIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("list active Go reachability facts: resolve scopes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var scopes, generations []string
	for rows.Next() {
		var scopeID, generationID string
		if err := rows.Scan(&scopeID, &generationID); err != nil {
			return nil, nil, fmt.Errorf("list active Go reachability facts: resolve scopes: %w", err)
		}
		scopes = append(scopes, scopeID)
		generations = append(generations, generationID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("list active Go reachability facts: resolve scopes: %w", err)
	}
	return scopes, generations, nil
}

// listGoReachabilityFactRows runs the (scope_id, generation_id, fact_kind)
// probe and decodes every live Go evidence envelope.
func listGoReachabilityFactRows(
	ctx context.Context,
	queryer db.Queryer,
	query string,
	scopes []string,
	generations []string,
) ([]facts.Envelope, error) {
	rows, err := queryer.QueryContext(ctx, query, scopes, generations, goReachabilityEvidenceKinds())
	if err != nil {
		return nil, fmt.Errorf("list active Go reachability facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var loaded []facts.Envelope
	for rows.Next() {
		envelope, err := backfill.ScanFactEnvelope(rows)
		if err != nil {
			return nil, fmt.Errorf("list active Go reachability facts: %w", err)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active Go reachability facts: %w", err)
	}
	return loaded, nil
}
