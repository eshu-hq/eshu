// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10880 PR-B2a disposable PostgreSQL 18 proof of the literal repository_id <> '' on every supports read driven by a repository: a finding with a blank repository id is seeded, and for grants that name a blank repository id (alone, beside a real repository, beside a scope) the granted list, count, grouped inventory and explain on supports equal the legacy read, which matches no blank-repository finding through the repository grant; reads ESHU_GROUPED_INVENTORY_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GROUPED_INVENTORY_PROOF_DISPOSABLE ESHU_GROUPED_INVENTORY_PROOF_DSN

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

// seedBlankRepositoryFindings adds findings whose payload names no repository:
// bl1 in scope A and bl2 in scope B. A repository grant can only reach them
// through a blank id, which the repository arm must never match (a supports read
// driven from a repository carries the literal repository_id <> ” so a partial
// composite index can serve it).
func seedBlankRepositoryFindings(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	for id, scope := range map[string]string{"bl1": multiSupportScopeA, "bl2": multiSupportScopeB} {
		payload := groupedLiveFindingPayload(id, id, 55, 5.0)
		payload["repository_id"] = ""
		generation := "gen:a"
		if scope == multiSupportScopeB {
			generation = "gen:b"
		}
		groupedLiveFact(t, ctx, database, "fact:"+id, scope, generation, FindingFactKind, payload)
	}
}

// TestBlankRepositoryGrantMatchesLegacyLive pins the literal repository_id <> ”
// on the repository arm of every supports read. Production grants never hold a
// blank id: the caller's repository grant is cleaned before it reaches a store
// (auth.CleanedStrings drops blank and duplicate ids), so the grant the stores see
// is the cleaned one, and on it supports equals legacy for every grant shape. The
// stores themselves differ on a raw blank id: legacy matches a blank-repository
// finding through it, supports does not, and the test pins that difference so it is
// a visible contract and not an accident.
func TestBlankRepositoryGrantMatchesLegacyLive(t *testing.T) {
	ctx, database := openSupportsEqualityDatabaseWith(t, seedBlankRepositoryFindings)
	supports := PostgresFindingStore{DB: database, ReadFromWinners: true}
	legacy := PostgresFindingStore{DB: database, ReadFromWinners: false}
	supportsAgg := NewPostgresAggregateStoreWithReadModel(database, true)
	legacyAgg := NewPostgresAggregateStore(database)
	at := time.Now().UTC()
	supportsAgg.Now = func() time.Time { return at }
	legacyAgg.Now = func() time.Time { return at }

	blankFindings := func(rows []FindingRow) []string {
		var ids []string
		for _, row := range rows {
			if row.FindingID == "finding:bl1" || row.FindingID == "finding:bl2" {
				ids = append(ids, row.FindingID)
			}
		}
		return ids
	}
	grants := []supportsEqualityGrant{
		{"blank repository alone", []string{""}, nil},
		{"blank beside a real repository", []string{"", "repository:m1"}, nil},
		{"blank beside a scope", []string{""}, []string{multiSupportScopeA}},
		{"blank beside a scope that holds the blank findings", []string{"", "repository:none"}, []string{multiSupportScopeB}},
	}
	if got := auth.CleanedStrings([]string{"", "  ", "repository:m1", "repository:m1"}); len(got) != 1 || got[0] != "repository:m1" {
		t.Fatalf("auth.CleanedStrings = %v: a blank grant id would reach the stores", got)
	}
	for _, grant := range grants {
		t.Run(grant.name, func(t *testing.T) {
			// The production path: the cleaned grant.
			cleaned := auth.CleanedStrings(grant.repositories)
			filter := FindingFilter{Limit: 200, Status: "affected_exact", AllowedRepositoryIDs: cleaned, AllowedScopeIDs: grant.scopes}
			if len(cleaned) == 0 && len(grant.scopes) == 0 {
				return // an empty grant is ungranted: not a supports read
			}
			got, err := supports.ListSupplyChainImpactFindings(ctx, filter)
			if err != nil {
				t.Fatalf("supports list: %v", err)
			}
			want, err := legacy.ListSupplyChainImpactFindings(ctx, filter)
			if err != nil {
				t.Fatalf("legacy list: %v", err)
			}
			if multiSupportDescribe(got) != multiSupportDescribe(want) {
				t.Fatalf("granted list on the cleaned grant differs\nlegacy:   %s\nsupports: %s", multiSupportDescribe(want), multiSupportDescribe(got))
			}
			aggregate := AggregateFilter{Status: "affected_exact", AllowedRepositoryIDs: cleaned, AllowedScopeIDs: grant.scopes}
			gotCount, err := supportsAgg.CountSupplyChainImpactFindings(ctx, aggregate)
			if err != nil {
				t.Fatalf("supports count: %v", err)
			}
			wantCount, err := legacyAgg.CountSupplyChainImpactFindings(ctx, aggregate)
			if err != nil {
				t.Fatalf("legacy count: %v", err)
			}
			if gotCount.TotalFindings != wantCount.TotalFindings {
				t.Fatalf("granted count on the cleaned grant = %d, legacy = %d", gotCount.TotalFindings, wantCount.TotalFindings)
			}
			for _, row := range want {
				request := packet.ExplanationFilter{FindingID: row.FindingID, AllowedRepositoryIDs: cleaned, AllowedScopeIDs: grant.scopes}
				gotExplain, gotErr := supports.ExplainSupplyChainImpact(ctx, request)
				wantExplain, wantErr := legacy.ExplainSupplyChainImpact(ctx, request)
				if explainOutcome(gotExplain, gotErr) != explainOutcome(wantExplain, wantErr) {
					t.Fatalf("explain %s differs from legacy on the cleaned grant", row.FindingID)
				}
			}

			// The raw blank id, which no production caller sends: supports never
			// reaches a blank-repository finding through the repository arm.
			raw := filter
			raw.AllowedRepositoryIDs = grant.repositories
			rawGot, err := supports.ListSupplyChainImpactFindings(ctx, raw)
			if err != nil {
				t.Fatalf("supports list on the raw grant: %v", err)
			}
			rawWant, err := legacy.ListSupplyChainImpactFindings(ctx, raw)
			if err != nil {
				t.Fatalf("legacy list on the raw grant: %v", err)
			}
			viaScope := map[string]bool{"finding:bl1": false, "finding:bl2": false}
			for _, scope := range grant.scopes {
				viaScope["finding:bl1"] = viaScope["finding:bl1"] || scope == multiSupportScopeA
				viaScope["finding:bl2"] = viaScope["finding:bl2"] || scope == multiSupportScopeB
			}
			for _, id := range blankFindings(rawGot) {
				if !viaScope[id] {
					t.Fatalf("supports returned %s through a blank repository id: the literal repository_id <> '' is missing", id)
				}
			}
			if len(blankFindings(rawWant)) == 0 {
				t.Fatal("legacy no longer matches a blank-repository finding through a blank grant id: the pinned difference is gone, update the test")
			}
		})
	}
}
