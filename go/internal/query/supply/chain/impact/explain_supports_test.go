// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

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
	// winnersAt is the winners watermark the fake answers; the zero value answers
	// no row (a winners table that was never materialized).
	winnersAt time.Time
}

func (q *explainRouteQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	q.queries = append(q.queries, query)
	if strings.Contains(query, "supply_chain_impact_finding_supports_backfill") {
		if q.probeErr != nil {
			return nil, q.probeErr
		}
		return &cov7StoreRows{rows: [][]any{{q.ready}}}, nil
	}
	if query == SelectWinnersWatermarkQuery {
		if q.winnersAt.IsZero() {
			return &cov7StoreRows{}, nil
		}
		return &cov7StoreRows{rows: [][]any{{q.winnersAt}}}, nil
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
	watermark := func(q string) bool { return q == SelectWinnersWatermarkQuery }
	winnersPicks := func(q string) bool {
		return strings.Contains(q, "FROM supply_chain_impact_canonical_winners AS w") &&
			strings.Contains(q, "FROM supply_chain_impact_finding_supports AS s") && !strings.Contains(q, "FROM fact_records AS fact")
	}
	imageSource := func(q string) bool {
		return strings.Contains(q, "image_ref = $10 AND image_ref <> ''") && !strings.Contains(q, "FROM fact_records AS fact")
	}

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
		"ungranted public id then winners identity fallback": {
			true,
			packet.ExplanationFilter{FindingID: "finding:a"},
			[]func(string) bool{probe, supportsPublicID, watermark, winnersPicks},
		},
		"ungranted selector reads the winners picks": {
			true,
			packet.ExplanationFilter{CVEID: "CVE-2026-1", PackageID: "pkg:npm/p"},
			[]func(string) bool{probe, watermark, winnersPicks},
		},
		"ungranted image selector reads the image index": {
			true,
			packet.ExplanationFilter{CVEID: "CVE-2026-1", ImageRef: "registry.example/a:1"},
			[]func(string) bool{probe, imageSource},
		},
		"gate off keeps legacy for a granted public id": {
			false,
			packet.ExplanationFilter{FindingID: "finding:a", AllowedScopeIDs: granted},
			[]func(string) bool{legacy, legacy},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := &explainRouteQueryer{ready: true, winnersAt: time.Now()}
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

// TestExplainWinnersSelectorRefusesWhenTheWinnersModelIsNotFresh pins the
// fail-closed rule of the unscoped selector explain: a key with supports but no
// winners row yet would read as "not found", so the winners watermark must be
// present and inside the freshness window, or the call answers the typed 503
// instead of a wrong not-found. The image selector reads supports only and does
// not need the winners table.
func TestExplainWinnersSelectorRefusesWhenTheWinnersModelIsNotFresh(t *testing.T) {
	t.Parallel()
	filter := packet.ExplanationFilter{CVEID: "CVE-2026-1", PackageID: "pkg:npm/p"}
	for name, at := range map[string]time.Time{
		"never materialized":    {},
		"older than the window": time.Now().Add(-2 * WinnersFreshnessWindow),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reader := &explainRouteQueryer{ready: true, winnersAt: at}
			_, err := NewPostgresFindingStoreWithReadStore(reader, true).ExplainSupplyChainImpact(t.Context(), filter)
			if !errors.Is(err, ErrExplanationWinnersUnavailable) {
				t.Fatalf("explain = %v, want ErrExplanationWinnersUnavailable", err)
			}
			for _, query := range reader.queries {
				if strings.Contains(query, "FROM supply_chain_impact_canonical_winners AS w") {
					t.Fatal("a refused explain read the winners table")
				}
			}
		})
	}
	reader := &explainRouteQueryer{ready: true}
	image := packet.ExplanationFilter{CVEID: "CVE-2026-1", ImageRef: "registry.example/a:1"}
	if _, err := NewPostgresFindingStoreWithReadStore(reader, true).ExplainSupplyChainImpact(t.Context(), image); !errors.Is(err, ErrExplanationNotFound) {
		t.Fatalf("image explain = %v, want not found: it must not need the winners watermark", err)
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
		"winners":   ExplainFindingFromWinnersQuery,
		"image":     ExplainFindingFromImageQuery,
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
