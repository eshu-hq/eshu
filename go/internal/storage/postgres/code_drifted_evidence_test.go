// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
	reducercodedivergence "github.com/eshu-hq/eshu/go/internal/reducer/codedivergence"
)

// TestPostgresCodeDriftedEvidenceLoaderAssemblesPairs proves the loader
// contract: band-pair rows join their member rows (entity fields plus
// decoded shingle sets), budget-exhausted entities flag from the partner
// counts, and pipeline stats carry through. A pair whose member row is
// missing never verifies; a member with an undecodable shingle set drops
// its pairs and counts them as no_shingles.
func TestPostgresCodeDriftedEvidenceLoaderAssemblesPairs(t *testing.T) {
	t.Parallel()

	shinglesA := fingerprint.EncodeShingles([]uint64{1, 2, 3, 4, 5, 6, 7, 8, 9})
	shinglesB := fingerprint.EncodeShingles([]uint64{1, 2, 3, 4, 5, 6, 7, 8, 101})
	fake := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				// e1, e2, shared, c1, c2: e1 holds 201 partners (exhausted).
				{"e1", "e2", 9, 201, 1},
				// e3's member row below carries a corrupt shingle hex.
				{"e1", "e3", 4, 201, 1},
				// e9 has no member row: the pair must drop.
				{"e2", "e9", 7, 1, 1},
			}},
			{rows: [][]any{
				{"e1", "exact-1", "renamed-1", shinglesA, 64, "big", "Function", "a.go", "go", 10, 40},
				{"e2", "exact-2", "renamed-2", shinglesB, 66, "bigCopy", "Function", "b.go", "go", 50, 80},
				{"e3", "exact-3", "renamed-3", "zz", 60, "third", "Function", "c.go", "go", 1, 30},
			}},
			{rows: [][]any{
				{2, 1},
			}},
			{rows: [][]any{
				{3},
			}},
		},
	}
	loader := PostgresCodeDriftedEvidenceLoader{DB: fake}
	page, err := loader.LoadCandidates(context.Background(), "repo-1")
	if err != nil {
		t.Fatalf("LoadCandidates() error = %v, want nil", err)
	}
	if len(page.Pairs) != 1 {
		t.Fatalf("pairs = %d, want 1 (corrupt-shingle and memberless pairs drop)", len(page.Pairs))
	}
	got := page.Pairs[0]
	if got.A.EntityID != "e1" || got.B.EntityID != "e2" || got.SharedBands != 9 {
		t.Fatalf("pair = %+v, want e1/e2 with 9 shared bands", got)
	}
	if len(got.A.Shingles) != 9 || len(got.B.Shingles) != 9 {
		t.Fatalf("shingle sets = %d/%d, want 9/9 decoded", len(got.A.Shingles), len(got.B.Shingles))
	}
	if got.A.EntityName != "big" || got.B.RelativePath != "b.go" || got.B.StartLine != 50 {
		t.Fatalf("member fields = %+v, want entity-mapped rows", got)
	}
	if len(page.Stats.BudgetExhausted) != 1 || page.Stats.BudgetExhausted[0] != "e1" {
		t.Fatalf("budget exhausted = %v, want [e1]", page.Stats.BudgetExhausted)
	}
	if page.Stats.BelowFloor != 2 || page.Stats.NoShingles != 3 || page.Stats.EqualityDuplicates != 3 {
		t.Fatalf("stats = %+v, want below_floor 2, no_shingles 1 row + 1 corrupt + 1 churned, equality 3", page.Stats)
	}
	if len(fake.queries) != 4 {
		t.Fatalf("queries issued = %d, want 4 (pairs, members, exclusions, equality)", len(fake.queries))
	}
	if args := fake.queries[0].args; len(args) < 3 || args[0] != "repo-1" {
		t.Fatalf("pairs query args = %v, want repo first", args)
	}
	if !strings.Contains(fake.queries[0].query, "code_fingerprint_band") {
		t.Fatal("pairs query must read the band side table, never source_cache")
	}
	if strings.Contains(fake.queries[0].query+fake.queries[1].query+fake.queries[2].query, "source_cache") {
		t.Fatal("candidate loading must never touch source_cache")
	}
}

// TestCodeDriftedQueriesCarryLoadBearingClauses is the hermetic shape guard
// for the candidate SQL: the band self-join predicate (served by the #6834
// lookup index), the per-entity budget window, the equality ownership
// exclusion, and the member ANY() lookup must survive refactoring. It reads
// the shipped constants, never a hand copy.
func TestCodeDriftedQueriesCarryLoadBearingClauses(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"code_fingerprint_band",
		"band_no",
		"band_hash",
		"ROW_NUMBER() OVER (PARTITION BY",
		"fp_renamed",
		"shingles IS NOT NULL",
	} {
		if !strings.Contains(listCodeDriftedPairsQuery, want) {
			t.Fatalf("pairs query missing load-bearing clause %q", want)
		}
	}
	for _, want := range []string{
		"code_function_fingerprint",
		"content_entities",
		"= ANY(",
	} {
		if !strings.Contains(listCodeDriftedMembersQuery, want) {
			t.Fatalf("members query missing load-bearing clause %q", want)
		}
	}
	for _, want := range []string{
		"code_function_fingerprint",
		"token_count",
	} {
		if !strings.Contains(countCodeDriftedExclusionsQuery, want) {
			t.Fatalf("exclusions query missing load-bearing clause %q", want)
		}
	}
	for _, want := range []string{
		"code_fingerprint_band",
		"fp_exact",
		"fp_renamed",
	} {
		if !strings.Contains(countCodeDriftedEqualityDuplicatesQuery, want) {
			t.Fatalf("equality query missing load-bearing clause %q", want)
		}
	}
}

var _ reducercodedivergence.CandidateLoader = PostgresCodeDriftedEvidenceLoader{}
