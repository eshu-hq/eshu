// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// explainRouteQueryer answers the supports readiness probe and returns no rows
// for every other statement, recording each. It is a plain db.Queryer, so the
// explain runs without a snapshot, like the other legacy test doubles.
type explainRouteQueryer struct {
	queries  []string
	ready    bool
	probeErr error
}

func (q *explainRouteQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	q.queries = append(q.queries, query)
	if strings.Contains(query, "supply_chain_impact_finding_supports_backfill") {
		if q.probeErr != nil {
			return nil, q.probeErr
		}
		return &cov7StoreRows{rows: [][]any{{q.ready}}}, nil
	}
	return &cov7StoreRows{}, nil
}

// The explain reads of #10880 PR-B2a: a granted call, and a lookup by public
// finding id, read the supports table behind the readiness probe; an ungranted
// selector or identity lookup keeps the legacy read; and a supports read that
// cannot be served refuses instead of falling back.
func TestExplainRoutesByGrantAndLookup(t *testing.T) {
	t.Parallel()
	granted := []string{"scope:a"}
	supportsPublicID := func(q string) bool {
		return strings.Contains(q, "fact.has_payload_finding_id = 1") && strings.Contains(q, "FROM supply_chain_impact_finding_supports AS fact")
	}
	supportsGrantArms := func(q string) bool {
		return strings.Contains(q, "FROM (SELECT * FROM supply_chain_impact_finding_supports WHERE scope_id = ANY($12::text[])")
	}
	legacy := func(q string) bool { return strings.Contains(q, "FROM fact_records AS fact") }
	probe := func(q string) bool { return strings.Contains(q, "supply_chain_impact_finding_supports_backfill") }

	for name, tc := range map[string]struct {
		gate   bool
		filter packet.ExplanationFilter
		want   []func(string) bool
	}{
		"granted selector": {
			true,
			packet.ExplanationFilter{CVEID: "CVE-2026-1", PackageID: "pkg:npm/p", AllowedScopeIDs: granted},
			[]func(string) bool{probe, supportsGrantArms},
		},
		"granted public id then identity fallback": {
			true,
			packet.ExplanationFilter{FindingID: "finding:a", AllowedScopeIDs: granted},
			[]func(string) bool{probe, supportsPublicID, supportsGrantArms},
		},
		"ungranted public id then legacy identity fallback": {
			true,
			packet.ExplanationFilter{FindingID: "finding:a"},
			[]func(string) bool{probe, supportsPublicID, legacy},
		},
		"ungranted selector stays legacy": {
			true,
			packet.ExplanationFilter{CVEID: "CVE-2026-1", PackageID: "pkg:npm/p"},
			[]func(string) bool{legacy},
		},
		"gate off keeps legacy for a granted public id": {
			false,
			packet.ExplanationFilter{FindingID: "finding:a", AllowedScopeIDs: granted},
			[]func(string) bool{legacy, legacy},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := &explainRouteQueryer{ready: true}
			store := NewPostgresFindingStoreWithReadStore(reader, tc.gate)
			if _, err := store.ExplainSupplyChainImpact(t.Context(), tc.filter); !errors.Is(err, ErrExplanationNotFound) {
				t.Fatalf("explain = %v, want ErrExplanationNotFound for an empty store", err)
			}
			if len(reader.queries) != len(tc.want) {
				t.Fatalf("issued %d statements, want %d", len(reader.queries), len(tc.want))
			}
			for i, check := range tc.want {
				if !check(reader.queries[i]) {
					t.Fatalf("statement %d is not the expected read:\n%s", i, reader.queries[i])
				}
			}
		})
	}
}

func TestExplainRefusesWhenSupportsAreNotReady(t *testing.T) {
	t.Parallel()
	for name, reader := range map[string]*explainRouteQueryer{
		"behind the contract or fenced": {ready: false},
		"readiness probe failed":        {probeErr: errors.New("relation does not exist")},
	} {
		for lookup, filter := range map[string]packet.ExplanationFilter{
			"granted selector": {CVEID: "CVE-2026-1", PackageID: "pkg:npm/p", AllowedRepositoryIDs: []string{"repo:a"}},
			"public id":        {FindingID: "finding:a"},
		} {
			t.Run(name+"/"+lookup, func(t *testing.T) {
				t.Parallel()
				reader := &explainRouteQueryer{ready: reader.ready, probeErr: reader.probeErr}
				store := NewPostgresFindingStoreWithReadStore(reader, true)
				_, err := store.ExplainSupplyChainImpact(t.Context(), filter)
				if !errors.Is(err, ErrFindingSupportsUnavailable) {
					t.Fatalf("explain = %v, want ErrFindingSupportsUnavailable", err)
				}
				if len(reader.queries) != 1 {
					t.Fatalf("a refused explain issued %d statements, want only the probe (never legacy)", len(reader.queries))
				}
			})
		}
	}
}

func TestExplainSupportsDerivationFailsOnALostMarker(t *testing.T) {
	t.Parallel()
	legacy := supplyChainImpactSplitCanonicalWinnerCTEs("$13::timestamptz")
	for name, mutate := range map[string]func(string) string{
		"operator overrides": func(q string) string { return strings.Replace(q, "operator_overrides AS (", "renamed AS (", 1) },
		"joined winners":     func(q string) string { return strings.Replace(q, "joined_winners AS (", "renamed AS (", 1) },
		"override join": func(q string) string {
			return strings.Replace(q, "LEFT JOIN operator_overrides AS override\n    ON override.canonical_key = source.canonical_key", "LEFT JOIN renamed AS override", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mutated := mutate(legacy)
			if mutated == legacy {
				t.Fatalf("mutation %q changed nothing: the marker text moved", name)
			}
			if _, err := deriveSupportsExplainWinners(mutated); err == nil {
				t.Fatalf("derivation accepted a winner CTE text that lost the %s marker", name)
			}
		})
	}
	if _, err := deriveSupportsExplainWinners(legacy); err != nil {
		t.Fatalf("derivation of the shipped winner CTEs failed: %v", err)
	}
}

// TestExplainSupportsReadsReferenceEveryBindAndOnlySupports proves every one of
// the 19 explain arguments is referenced (Postgres rejects an untyped one) and
// that no finding row is read from the fact table.
func TestExplainSupportsReadsReferenceEveryBindAndOnlySupports(t *testing.T) {
	t.Parallel()
	for name, query := range map[string]string{
		"public id": ExplainFindingByPublicIDFromSupportsQuery,
		"selector":  ExplainFindingFromSupportsQuery,
	} {
		for n := 1; n <= 19; n++ {
			token := fmt.Sprintf("$%d", n)
			found := false
			for _, at := range indexAll(query, token) {
				if next := at + len(token); next >= len(query) || query[next] < '0' || query[next] > '9' {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s read never references %s", name, token)
			}
		}
		for _, banned := range []string{"FROM fact_records AS fact", "operator_candidates", "operator_overrides AS"} {
			if strings.Contains(query, banned) {
				t.Errorf("%s read still contains %q", name, banned)
			}
		}
	}
}
