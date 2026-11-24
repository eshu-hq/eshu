// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-B2a disposable PostgreSQL 18 proof of the supports-sourced explain on the multi-support corpus: for every grant shape and non-rank filter, each finding id the granted list returns explains for the same caller with the same payload (the list-then-explain round trip), the granted explain equals the legacy explain for each of those ids, an ungranted public-id lookup equals the legacy read, a granted selector lookup equals the legacy read, and a supports table that is not ready refuses instead of answering from the facts; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

func explainOutcome(row ExplanationRow, err error) string {
	switch {
	case errors.Is(err, ErrExplanationNotFound):
		return "not found"
	case errors.Is(err, ErrExplanationAmbiguous):
		return "ambiguous"
	case err != nil:
		return "error: " + err.Error()
	}
	encoded, _ := json.Marshal(row)
	return string(encoded)
}

// TestExplainFromSupportsLive holds the supports explain to two contracts: any
// finding id a granted list returned explains for the same caller (arbiter
// ruling for #10880 PR-B, decision 3) with the list row's payload, except that a
// list call with an existence filter (image_ref, profile) shows the best support
// matching the filter while the explain shows the canonical pick; and the
// explain equals the legacy explain wherever both are defined.
func TestExplainFromSupportsLive(t *testing.T) {
	ctx, database := openSupportsEqualityDatabase(t)
	list := PostgresFindingStore{DB: database, ReadFromWinners: true}
	supports := PostgresFindingStore{DB: database, ReadFromWinners: true}
	legacy := PostgresFindingStore{DB: database, ReadFromWinners: false}

	explained, compared := 0, 0
	for _, grant := range supportsEqualityGrants() {
		for _, tc := range supportsEqualityFilters() {
			if tc.rank || tc.filter.Limit != 0 {
				continue
			}
			t.Run(grant.name+"/"+tc.name, func(t *testing.T) {
				filter := tc.filter
				filter.AllowedRepositoryIDs, filter.AllowedScopeIDs = grant.repositories, grant.scopes
				if filter.Status == "" {
					filter.Status = "affected_exact"
				}
				filter.Limit = 200
				rows, err := list.ListSupplyChainImpactFindings(ctx, filter)
				if err != nil {
					t.Fatalf("granted list: %v", err)
				}
				for _, row := range rows {
					request := packet.ExplanationFilter{
						FindingID:            row.FindingID,
						AllowedRepositoryIDs: grant.repositories, AllowedScopeIDs: grant.scopes,
					}
					got, err := supports.ExplainSupplyChainImpact(ctx, request)
					if err != nil {
						t.Fatalf("list-then-explain: finding %q the granted list returned did not explain: %v", row.FindingID, err)
					}
					listed, _ := json.Marshal(row)
					explainedRow, _ := json.Marshal(got.Finding)
					if tc.filter.ImageRef != "" || tc.filter.DetectionProfile != "" {
						// An existence filter picks among the supports that match it
						// (any-support, filter then pick), so the listed row can be a
						// lower-ranked support than the canonical pick the explain
						// returns. The finding is the same one: its identity holds.
						if got.Finding.FindingID != row.FindingID || got.Finding.CVEID != row.CVEID ||
							got.Finding.PackageID != row.PackageID || got.Finding.RepositoryID != row.RepositoryID ||
							got.Finding.Status != row.Status {
							t.Fatalf("finding %q: the explain names a different finding\nlist:    %s\nexplain: %s", row.FindingID, listed, explainedRow)
						}
					} else if string(listed) != string(explainedRow) {
						t.Fatalf("finding %q: the explain payload differs from the list row\nlist:    %s\nexplain: %s", row.FindingID, listed, explainedRow)
					}
					want, wantErr := legacy.ExplainSupplyChainImpact(ctx, request)
					if explainOutcome(got, nil) != explainOutcome(want, wantErr) {
						t.Fatalf("finding %q: the supports explain differs from legacy\nlegacy:   %s\nsupports: %s",
							row.FindingID, explainOutcome(want, wantErr), explainOutcome(got, nil))
					}
					explained++
				}
				compared++
			})
		}
	}
	if compared == 0 || explained == 0 {
		t.Fatalf("compared %d cases and explained %d findings: the matrix lost its signal", compared, explained)
	}
}

// TestExplainFromSupportsSelectorAndPublicIDLive pins the lookups that do not
// start from a list: a granted selector lookup, an ungranted public-id lookup,
// and the refusal while the table is not ready.
func TestExplainFromSupportsSelectorAndPublicIDLive(t *testing.T) {
	ctx, database := openSupportsEqualityDatabase(t)
	supports := PostgresFindingStore{DB: database, ReadFromWinners: true}
	legacy := PostgresFindingStore{DB: database, ReadFromWinners: false}

	all, err := legacy.ListSupplyChainImpactFindings(ctx, FindingFilter{Status: "affected_exact", Limit: 200})
	if err != nil || len(all) == 0 {
		t.Fatalf("seed list = %d rows, %v", len(all), err)
	}
	t.Run("ungranted public id equals the legacy read", func(t *testing.T) {
		for _, row := range all {
			request := packet.ExplanationFilter{FindingID: row.FindingID}
			got, gotErr := supports.ExplainSupplyChainImpact(ctx, request)
			want, wantErr := legacy.ExplainSupplyChainImpact(ctx, request)
			if explainOutcome(got, gotErr) != explainOutcome(want, wantErr) {
				t.Fatalf("finding %q differs from legacy\nlegacy:   %s\nsupports: %s", row.FindingID,
					explainOutcome(want, wantErr), explainOutcome(got, gotErr))
			}
		}
	})
	t.Run("granted public id equals the legacy read for every id, in or out of the grant", func(t *testing.T) {
		notFound, found := 0, 0
		for _, grant := range supportsEqualityGrants() {
			for _, row := range all {
				request := packet.ExplanationFilter{
					FindingID:            row.FindingID,
					AllowedRepositoryIDs: grant.repositories, AllowedScopeIDs: grant.scopes,
				}
				got, gotErr := supports.ExplainSupplyChainImpact(ctx, request)
				want, wantErr := legacy.ExplainSupplyChainImpact(ctx, request)
				if explainOutcome(got, gotErr) != explainOutcome(want, wantErr) {
					t.Fatalf("grant %q finding %q differs from legacy\nlegacy:   %s\nsupports: %s", grant.name, row.FindingID,
						explainOutcome(want, wantErr), explainOutcome(got, gotErr))
				}
				if errors.Is(gotErr, ErrExplanationNotFound) {
					notFound++
				} else {
					found++
				}
			}
		}
		if notFound == 0 || found == 0 {
			t.Fatalf("found %d, not found %d: the matrix needs ids both inside and outside a grant", found, notFound)
		}
	})
	t.Run("granted selector lookup equals the legacy read", func(t *testing.T) {
		checked := 0
		for _, grant := range supportsEqualityGrants() {
			for _, row := range all {
				request := packet.ExplanationFilter{
					CVEID: row.CVEID, PackageID: row.PackageID, RepositoryID: row.RepositoryID,
					AllowedRepositoryIDs: grant.repositories, AllowedScopeIDs: grant.scopes,
				}
				if !request.HasBoundedScope() {
					continue
				}
				got, gotErr := supports.ExplainSupplyChainImpact(ctx, request)
				want, wantErr := legacy.ExplainSupplyChainImpact(ctx, request)
				if explainOutcome(got, gotErr) != explainOutcome(want, wantErr) {
					t.Fatalf("grant %q selector %s/%s/%s differs from legacy\nlegacy:   %s\nsupports: %s", grant.name,
						row.CVEID, row.PackageID, row.RepositoryID, explainOutcome(want, wantErr), explainOutcome(got, gotErr))
				}
				checked++
			}
		}
		if checked == 0 {
			t.Fatal("no selector lookup was checked")
		}
	})
	t.Run("a supports table that is not ready refuses", func(t *testing.T) {
		if _, err := database.ExecContext(ctx, `INSERT INTO supply_chain_impact_finding_supports_dirty (scope_id) VALUES ('scope:dirty')`); err != nil {
			t.Fatalf("fence: %v", err)
		}
		_, granted := supports.ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{
			FindingID: all[0].FindingID, AllowedScopeIDs: []string{multiSupportScopeA},
		})
		_, publicID := supports.ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{FindingID: all[0].FindingID})
		if _, err := database.ExecContext(ctx, `DELETE FROM supply_chain_impact_finding_supports_dirty`); err != nil {
			t.Fatalf("restore: %v", err)
		}
		if !errors.Is(granted, ErrFindingSupportsUnavailable) || !errors.Is(publicID, ErrFindingSupportsUnavailable) {
			t.Fatalf("not-ready explain = %v / %v, want ErrFindingSupportsUnavailable for both", granted, publicID)
		}
	})
}
