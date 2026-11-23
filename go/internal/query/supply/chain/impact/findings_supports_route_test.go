// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The granted list read (#10880 PR-B2a) is served from the per-support table
// when the winners gate is on: the winners row is one pick across scopes, so it
// cannot answer a grant (arbiter ruling prb, decision 1). The read refuses
// instead of falling back (decision 1, "no silent fallback"): a supports table
// that is missing, behind the binary's contract version, or still fenced answers
// ErrFindingSupportsUnavailable and never the winners row or the legacy facts.
// These tests pin the routing and the refusal with a recording queryer.

// supportsRouteQueryer answers the readiness probe with ready (or an error) and
// every other statement with an empty page, and records the statements.
func supportsRouteQueryer(ready bool, probeErr error) *cov7StoreQueryer {
	return &cov7StoreQueryer{onRows: func(query string) (db.Rows, error) {
		if strings.Contains(query, "supply_chain_impact_finding_supports_backfill") {
			if probeErr != nil {
				return nil, probeErr
			}
			return &cov7StoreRows{rows: [][]any{{ready}}}, nil
		}
		return &cov7StoreRows{}, nil
	}}
}

func TestFindingListGrantedCallReadsSupportsWhenReady(t *testing.T) {
	t.Parallel()
	for name, filter := range map[string]FindingFilter{
		"scope grant":      {Limit: 10, Status: "affected_exact", AllowedScopeIDs: []string{"scope:a"}},
		"repository grant": {Limit: 10, CVEID: "CVE-2026-1", AllowedRepositoryIDs: []string{"repo:a"}},
		"precise profile":  {Limit: 10, Status: "affected_exact", DetectionProfile: ProfilePrecise, AllowedScopeIDs: []string{"scope:a"}},
		"image ref":        {Limit: 10, ImageRef: "registry.example/app:1", AllowedRepositoryIDs: []string{"repo:a"}},
		"rank filter":      {Limit: 10, Severity: "high", AllowedScopeIDs: []string{"scope:a"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			probe := supportsRouteQueryer(true, nil)
			store := NewPostgresFindingStoreWithReadStore(probe, true)
			if _, err := store.ListSupplyChainImpactFindings(t.Context(), filter); err != nil {
				t.Fatalf("list = %v, want a page", err)
			}
			if len(probe.queries) != 2 {
				t.Fatalf("granted list issued %d statements, want the readiness probe and one list read", len(probe.queries))
			}
			list := probe.queries[1]
			if !strings.Contains(list, "FROM supply_chain_impact_finding_supports") ||
				strings.Contains(list, "FROM fact_records AS fact") ||
				strings.Contains(list, "supply_chain_impact_canonical_winners AS w") {
				t.Fatalf("granted gate-on list did not read the supports table:\n%s", list)
			}
		})
	}
}

func TestFindingListGrantedCallRefusesWhenSupportsAreNotReady(t *testing.T) {
	t.Parallel()
	for name, probe := range map[string]*cov7StoreQueryer{
		"behind the contract or fenced": supportsRouteQueryer(false, nil),
		"readiness probe failed":        supportsRouteQueryer(false, errors.New("relation does not exist")),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := NewPostgresFindingStoreWithReadStore(probe, true)
			_, err := store.ListSupplyChainImpactFindings(t.Context(),
				FindingFilter{Limit: 10, Status: "affected_exact", AllowedScopeIDs: []string{"scope:a"}})
			if !errors.Is(err, ErrFindingSupportsUnavailable) {
				t.Fatalf("list = %v, want ErrFindingSupportsUnavailable", err)
			}
			if len(probe.queries) != 1 {
				t.Fatalf("refused list issued %d statements, want only the readiness probe (never winners or legacy)", len(probe.queries))
			}
		})
	}
}

func TestFindingListGateOffKeepsTheLegacyReadWithoutAProbe(t *testing.T) {
	t.Parallel()
	probe := supportsRouteQueryer(false, nil)
	store := NewPostgresFindingStoreWithReadStore(probe, false)
	if _, err := store.ListSupplyChainImpactFindings(t.Context(),
		FindingFilter{Limit: 10, Status: "affected_exact", AllowedScopeIDs: []string{"scope:a"}}); err != nil {
		t.Fatalf("list = %v, want a page", err)
	}
	if len(probe.queries) != 1 || !strings.Contains(probe.queries[0], "FROM fact_records AS fact") {
		t.Fatalf("gate-off granted list = %d statements, want one legacy fact read with no probe", len(probe.queries))
	}
}

func TestFindingListUnscopedPlainCallStillReadsWinners(t *testing.T) {
	t.Parallel()
	probe := supportsRouteQueryer(false, nil)
	store := NewPostgresFindingStoreWithReadStore(probe, true)
	if _, err := store.ListSupplyChainImpactFindings(t.Context(), FindingFilter{Limit: 10, Status: "affected_exact"}); err != nil {
		t.Fatalf("list = %v, want a page", err)
	}
	if len(probe.queries) != 1 || !strings.Contains(probe.queries[0], "supply_chain_impact_canonical_winners AS w") {
		t.Fatalf("unscoped plain gate-on list = %d statements, want one winners read with no supports probe", len(probe.queries))
	}
}

// TestFindingFreshnessFollowsTheRoute proves the freshness probe follows the
// route: a call answered from the supports table is always current and reports
// no winners freshness, while a winners-served call probes the watermark.
func TestFindingFreshnessFollowsTheRoute(t *testing.T) {
	t.Parallel()
	watermark := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	probe := &cov7StoreQueryer{onRows: func(query string) (db.Rows, error) {
		if query == SelectWinnersWatermarkQuery {
			return &cov7StoreRows{rows: [][]any{{watermark}}}, nil
		}
		return &cov7StoreRows{}, nil
	}}
	store := NewPostgresFindingStoreWithReadStore(probe, true)

	granted, err := store.SupplyChainImpactFreshness(t.Context(), FindingFilter{Limit: 10, Status: "affected_exact", AllowedScopeIDs: []string{"scope:a"}})
	if err != nil || granted.ServingFromWinners {
		t.Fatalf("granted freshness = %+v, %v; want the supports route (not winners)", granted, err)
	}
	if len(probe.queries) != 0 {
		t.Fatalf("granted freshness issued %d statements, want none", len(probe.queries))
	}
	plain, err := store.SupplyChainImpactFreshness(t.Context(), FindingFilter{Limit: 10, Status: "affected_exact"})
	if err != nil || !plain.ServingFromWinners || !plain.Present || !plain.MaterializedAt.Equal(watermark) {
		t.Fatalf("plain freshness = %+v, %v; want the winners watermark", plain, err)
	}
}
