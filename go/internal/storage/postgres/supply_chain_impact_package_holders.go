// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// findPackageHolderCandidatesQuery returns the active affected-package
// identities, outside the asking scope, whose folded ecosystem spelling is one
// of $1 (every alias of the requested ecosystems, see ecosystemSpellings): the
// #9874 package-keyed holder read. No index anchors affected_package by
// (ecosystem, name), so the ecosystem is a residual filter. With few scopes
// the planner probes each scope's active generation through
// fact_records_vulnerability_affected_package_active_scan_idx; with many
// scopes it bitmap-scans that whole partial index, superseded rows included,
// and joins to the active generation afterwards, so cost grows with every
// retained affected-package row of the requested ecosystems. The handler
// matches names with the findings pass's own consumption-key join, so this
// read stays a coarse prefilter and never under-matches a name spelling.
// There is no DISTINCT and no ORDER BY on purpose: the text sort was most of
// the statement at 10x; the store dedupes.
//
// Performance Evidence: docs/internal/evidence/9874-lockfile-retrigger.md
// (PG 18.6, synthetic 50,000 active plus 50,000 superseded affected-package
// rows: median 46.6 ms over 1,101 scopes, median 65.3 ms over 20,601 scopes;
// under the 10 s holder-read statement timeout).
const findPackageHolderCandidatesQuery = `
SELECT fact.scope_id,
       COALESCE(fact.payload->>'ecosystem', ''),
       COALESCE(fact.payload->>'package_name', ''),
       COALESCE(fact.payload->>'purl', ''),
       COALESCE(fact.payload->>'package_id', '')
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
WHERE fact.fact_kind = 'vulnerability.affected_package'
  AND fact.is_tombstone = FALSE
  AND fact.scope_id <> $2
  AND (LOWER(BTRIM(fact.payload->>'ecosystem') COLLATE "C") COLLATE "default") = ANY($1::text[])
`

// findAlertHolderCandidatesQuery returns the active repository-alert
// identities (#10061), outside the asking scope, whose payload repository_id
// is one of $1: the security-alert arm of the package-keyed holder read. The
// repository predicate is the leading expression of
// fact_records_security_alert_repository_lookup_idx (migration 003), so the
// read seeks the generation repository's alerts and joins to the active
// generation afterwards; no migration is needed. Alerts a provider already
// closed (dismissed, auto_dismissed, fixed — the set
// securityAlertCanSeedImpact refuses to seed a finding from) are left out:
// reopening their scope could not change a finding. Shape matches
// findPackageHolderCandidatesQuery (same five identity columns, no DISTINCT,
// no ORDER BY; the store dedupes), and the handler matches names with the
// findings pass's own consumption-key join, so an alert for an unchanged
// package of the same repository is filtered there, not here.
//
// Performance Evidence: docs/internal/evidence/10061-alert-scope-reopen.md
// (threshold stated before the probe there).
const findAlertHolderCandidatesQuery = `
SELECT fact.scope_id,
       COALESCE(fact.payload->>'ecosystem', ''),
       COALESCE(fact.payload->>'package_name', ''),
       COALESCE(fact.payload->>'purl', ''),
       COALESCE(fact.payload->>'package_id', '')
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
WHERE fact.fact_kind = 'security_alert.repository_alert'
  AND fact.is_tombstone = FALSE
  AND fact.scope_id <> $2
  AND fact.payload->>'repository_id' = ANY($1::text[])
  AND LOWER(BTRIM(COALESCE(fact.payload->>'provider_state', ''))) NOT IN ('dismissed', 'auto_dismissed', 'fixed')
`

// FindPackageHolderCandidates returns the active package-holder identities
// outside ownScopeID the query selects, deduplicated: affected-package rows
// in the query ecosystems, plus repository-alert rows for the query
// repositories. It runs as one read-only transaction under the holder-read
// statement timeout and fails with supplychaincore.ErrFanOutReadTimeout when
// the timeout fires. An empty query returns nothing without touching the
// database.
func (s SupplyChainImpactFanOutStore) FindPackageHolderCandidates(
	ctx context.Context,
	ownScopeID string,
	query supplychaincore.PackageHolderQuery,
) ([]supplychaincore.PackageHolderCandidate, error) {
	folded := ecosystemSpellings(query.Ecosystems)
	repositories := uniqueNonEmptyStrings(query.RepositoryIDs)
	if len(folded) == 0 && len(repositories) == 0 {
		return nil, nil
	}
	tx, err := s.beginBoundedRead(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	seen := map[supplychaincore.PackageHolderCandidate]struct{}{}
	var out []supplychaincore.PackageHolderCandidate
	collect := func(statement string, keys []string) error {
		rows, err := tx.QueryContext(ctx, statement, keys, ownScopeID)
		if err != nil {
			return classifyFanOutReadError(err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var candidate supplychaincore.PackageHolderCandidate
			if err := rows.Scan(&candidate.ScopeID, &candidate.Ecosystem, &candidate.PackageName,
				&candidate.PURL, &candidate.PackageID); err != nil {
				return fmt.Errorf("scan supply chain impact package holder: %w", err)
			}
			if _, dup := seen[candidate]; dup {
				continue
			}
			seen[candidate] = struct{}{}
			out = append(out, candidate)
		}
		if err := rows.Err(); err != nil {
			return classifyFanOutReadError(err)
		}
		return nil
	}
	if len(folded) > 0 {
		if err := collect(findPackageHolderCandidatesQuery, folded); err != nil {
			return nil, err
		}
	}
	if len(repositories) > 0 {
		if err := collect(findAlertHolderCandidatesQuery, repositories); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ecosystemSpellings expands each requested ecosystem to every spelling
// packageidentity.NormalizeEcosystem maps to the same canonical ecosystem.
// Sources store raw spellings (a GitLab Gemnasium slug writes go, packagist,
// gem), and the handler's consumption-key join normalizes them, so binding
// only the canonical name would under-match (#9874 F1). An unknown ecosystem
// contributes nothing: the join could not match it either.
func ecosystemSpellings(ecosystems []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, ecosystem := range ecosystems {
		for _, spelling := range packageidentity.EcosystemAliases(packageidentity.Ecosystem(ecosystem)) {
			if _, dup := seen[spelling]; !dup {
				seen[spelling] = struct{}{}
				out = append(out, spelling)
			}
		}
	}
	return out
}

// beginBoundedRead opens the read-only transaction every #9874 read runs in,
// with the holder-read statement timeout scoped to it, the same shape
// readScopeIDs uses for the CVE-keyed reads.
func (s SupplyChainImpactFanOutStore) beginBoundedRead(ctx context.Context) (db.Transaction, error) {
	if s.Beginner == nil {
		return nil, errors.New("supply chain impact fan-out beginner is required")
	}
	timeout := s.ReadTimeout
	if timeout <= 0 {
		timeout = supplyChainImpactFanOutReadTimeout
	}
	tx, err := s.Beginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin supply chain impact fan-out read: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "SET TRANSACTION READ ONLY"); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("set supply chain impact fan-out read read-only: %w", err)
	}
	rows, err := tx.QueryContext(ctx, setSupplyChainImpactFanOutReadTimeoutQuery, fmt.Sprintf("%dms", timeout.Milliseconds()))
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("set supply chain impact fan-out read timeout: %w", err)
	}
	_ = rows.Close()
	return tx, nil
}
