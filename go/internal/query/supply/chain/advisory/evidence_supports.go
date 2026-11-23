// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package advisory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// ErrEvidenceSupportsUnavailable is the sentinel a repository, service, or
// workload anchored advisory evidence call answers when the winners gate is on
// and the per-support table cannot serve it exactly: the readiness predicate is
// false or its probe failed (#10880 PR-B2a). The handler answers the 503
// backend_unavailable envelope. There is no fallback to the legacy fact read.
var ErrEvidenceSupportsUnavailable = errors.New(
	"advisory evidence supports read model is unavailable",
)

// ListEvidenceFromSupportsQuery is ListEvidenceQuery with the two reads of impact
// finding facts, the repository probe and the active-generation scan, swapped for
// reads of supply_chain_impact_finding_supports. The advisory facts themselves
// (vulnerability.*) still read from the fact table. Advisory evidence is
// per-support (arbiter ruling for #10880 PR-B, decision 3): the advisory and
// package anchors of every live support in the selected scope feed the lookup, as
// every live finding fact did, so a key whose supports carry different anchors
// contributes all of them.
//
// It is derived from the shipped constant, not copied; a marker the legacy text
// lost fails the derivation at package initialization.
var ListEvidenceFromSupportsQuery = mustDeriveEvidenceSupportsQuery(ListEvidenceQuery)

func mustDeriveEvidenceSupportsQuery(legacy string) string {
	query, err := deriveEvidenceSupportsQuery(legacy)
	if err != nil {
		panic(err)
	}
	return query
}

func deriveEvidenceSupportsQuery(legacy string) (string, error) {
	const repositoryProbe = `    JOIN fact_records AS fact
      ON fact.fact_kind = 'reducer_supply_chain_impact_finding'
     AND fact.is_tombstone = FALSE
     AND fact.payload->>'repository_id' = selector.repository_id
`
	const scopeScan = `        JOIN fact_records AS fact
          ON fact.scope_id = scope.scope_id
         AND scope.active_generation_id = fact.generation_id
        WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
          AND fact.is_tombstone = FALSE
`
	for name, marker := range map[string]string{"repository probe": repositoryProbe, "scope scan": scopeScan} {
		if strings.Count(legacy, marker) != 1 {
			return "", fmt.Errorf("legacy advisory evidence query lost the %s marker: update the supports derivation", name)
		}
	}
	query := strings.Replace(legacy, repositoryProbe, `    JOIN supply_chain_impact_finding_supports AS fact
      ON fact.repository_id = selector.repository_id
`, 1)
	return strings.Replace(query, scopeScan, `        JOIN supply_chain_impact_finding_supports AS fact
          ON fact.scope_id = scope.scope_id
         AND scope.active_generation_id = fact.generation_id
`, 1), nil
}

// readsFromSupports reports whether one call derives its anchors from impact
// findings, and so reads the per-support table when the gate is on. A bare
// cve_id, advisory_id, or package_id call reads no impact finding.
func (s PostgresEvidenceStore) readsFromSupports(filter EvidenceFilter) bool {
	return s.ReadFromWinners && filter.hasImpactScope()
}

// requireSupportsReady refuses a supports-served call while the table is not
// ready. A probe failure refuses too: a readiness error must never read as ready.
func requireSupportsReady(ctx context.Context, queryer db.Queryer) error {
	ready, err := storagepostgres.SupplyChainImpactSupportsReady(ctx, queryer)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrEvidenceSupportsUnavailable, err)
	}
	if !ready {
		return ErrEvidenceSupportsUnavailable
	}
	return nil
}
