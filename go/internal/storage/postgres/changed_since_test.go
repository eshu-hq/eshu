// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func scopeRow(scopeID, scopeKind, currentGen string, currentObserved any, hasPending bool, repository ...string) [][]any {
	resolvedRepository := scopeID
	if len(repository) > 0 {
		resolvedRepository = repository[0]
	}
	return [][]any{{scopeID, scopeKind, resolvedRepository, currentGen, currentObserved, hasPending}}
}

func priorRow(generationID string, observed any) [][]any {
	return [][]any{{generationID, observed}}
}

func TestComputeChangedSinceDeltaRejectsInvalidScopeSelectorsBeforeRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filter     statuspkg.ChangedSinceFilter
		wantErrSub string
	}{
		{
			name: "missing selector",
			filter: statuspkg.ChangedSinceFilter{
				SinceGenerationID: "gen-prior",
			},
			wantErrSub: "scope_id or repository is required",
		},
		{
			name: "conflicting selectors",
			filter: statuspkg.ChangedSinceFilter{
				Repository:        "repository:r_b",
				ScopeID:           "git-repository-scope:old",
				SinceGenerationID: "gen-prior",
			},
			wantErrSub: "scope_id and repository are mutually exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			queryer := &fakeQueryer{}
			store := NewStatusStore(queryer)
			_, err := store.ComputeChangedSinceDelta(context.Background(), tt.filter)
			if err == nil || !strings.Contains(err.Error(), tt.wantErrSub) {
				t.Fatalf("ComputeChangedSinceDelta() error = %v, want containing %q", err, tt.wantErrSub)
			}
			if len(queryer.queries) != 0 {
				t.Fatalf("queries = %d, want 0 before invalid selector rejection", len(queryer.queries))
			}
		})
	}
}

// diffRow builds one row of the single-statement changed-since diff: a bucket
// (category, classification, key_count) joined to one bounded sample handle.
func diffRow(category, classification string, count int64, key, kind string) []any {
	return []any{category, classification, count, key, kind}
}

// diffBucketOnlyRow builds a bucket row whose LATERAL sample join found no key,
// which the SQL emits as NULL sample columns.
func diffBucketOnlyRow(category, classification string, count int64) []any {
	return []any{category, classification, count, nil, nil}
}

func TestComputeChangedSinceDeltaUnchangedProducesNoFalseDeltas(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	prior := observed.Add(-time.Hour)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: scopeRow("git-repository-scope:acme/app", "repository", "gen-current", observed, false)},
		{rows: priorRow("gen-prior", prior)},
		// One statement returns every bucket count and its bounded samples.
		{rows: [][]any{
			diffRow("content_entities", "unchanged", 8, "entity:a", "content_entity"),
			diffRow("facts", "unchanged", 4, "fact:a", "aws_resource"),
			diffRow("files", "unchanged", 12, "file:a", "file"),
		}},
	}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeChangedSinceDelta(context.Background(), statuspkg.ChangedSinceFilter{
		Repository:        "acme/app",
		SinceGenerationID: "gen-prior",
		SampleLimit:       25,
	})
	if err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	if summary.Unavailable {
		t.Fatalf("Unavailable = true, want false")
	}
	if summary.SinceGenerationID != "gen-prior" || summary.CurrentActiveGenerationID != "gen-current" {
		t.Fatalf("unexpected generations: %+v", summary)
	}
	for _, category := range summary.Categories {
		c := category.Counts
		if c.Added != 0 || c.Updated != 0 || c.Retired != 0 || c.Superseded != 0 {
			t.Fatalf("category %s has false deltas: %+v", category.Category, c)
		}
		if category.Unavailable {
			t.Fatalf("category %s marked unavailable", category.Category)
		}
	}
}

func TestComputeChangedSinceDeltaClassifiesAllVerdicts(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 11, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: scopeRow("git-repository-scope:acme/app", "repository", "gen-current", observed, false)},
		{rows: priorRow("gen-prior", observed.Add(-time.Hour))},
		{rows: [][]any{
			diffRow("files", "added", 2, "file:new1", "file"),
			diffRow("files", "added", 2, "file:new2", "file"),
			diffRow("files", "retired", 1, "file:gone", "file"),
			diffRow("files", "superseded", 1, "file:dropped", "file"),
			diffRow("files", "unchanged", 3, "file:same", "file"),
			diffRow("files", "updated", 1, "file:upd", "file"),
		}},
	}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeChangedSinceDelta(context.Background(), statuspkg.ChangedSinceFilter{
		ScopeID:           "git-repository-scope:acme/app",
		SinceGenerationID: "gen-prior",
		SampleLimit:       25,
	})
	if err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}

	var files statuspkg.ChangedSinceCategoryDelta
	for _, category := range summary.Categories {
		if category.Category == statuspkg.ChangedSinceCategoryFiles {
			files = category
		}
	}
	c := files.Counts
	if c.Added != 2 || c.Updated != 1 || c.Unchanged != 3 || c.Retired != 1 || c.Superseded != 1 {
		t.Fatalf("files counts wrong: %+v", c)
	}
	if got := files.Samples[statuspkg.ChangedSinceAdded]; len(got) != 2 {
		t.Fatalf("added samples = %d, want 2", len(got))
	}
	if got := files.Samples[statuspkg.ChangedSinceRetired]; len(got) != 1 || got[0].StableFactKey != "file:gone" {
		t.Fatalf("retired samples wrong: %+v", got)
	}
	// #7127: the whole diff is one statement, so a request is scope + prior
	// generation + one diff round trip regardless of how many buckets matched.
	if got, want := len(queryer.queries), 3; got != want {
		t.Fatalf("queries = %d, want %d (scope, prior generation, one diff)", got, want)
	}
	if got := files.Samples[statuspkg.ChangedSinceSuperseded]; len(got) != 1 || got[0].StableFactKey != "file:dropped" {
		t.Fatalf("superseded samples wrong: %+v", got)
	}
}

func TestComputeChangedSinceDeltaTruncatesSamples(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	// limit 2 -> fetch 3 rows -> truncated true, trimmed to 2.
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: scopeRow("git-repository-scope:acme/app", "repository", "gen-current", observed, false)},
		{rows: priorRow("gen-prior", observed.Add(-time.Hour))},
		{rows: [][]any{
			diffRow("facts", "added", 5, "fact:a", "k"),
			diffRow("facts", "added", 5, "fact:b", "k"),
			diffRow("facts", "added", 5, "fact:c", "k"),
		}},
	}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeChangedSinceDelta(context.Background(), statuspkg.ChangedSinceFilter{
		ScopeID:           "git-repository-scope:acme/app",
		SinceGenerationID: "gen-prior",
		SampleLimit:       2,
	})
	if err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	var facts statuspkg.ChangedSinceCategoryDelta
	for _, category := range summary.Categories {
		if category.Category == statuspkg.ChangedSinceCategoryFacts {
			facts = category
		}
	}
	if len(facts.Samples[statuspkg.ChangedSinceAdded]) != 2 {
		t.Fatalf("added samples = %d, want 2 (trimmed)", len(facts.Samples[statuspkg.ChangedSinceAdded]))
	}
	if !facts.Truncated[statuspkg.ChangedSinceAdded] {
		t.Fatalf("added truncated = false, want true")
	}
}

func TestComputeChangedSinceDeltaUnknownScopeReturnsEmpty(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{{}}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeChangedSinceDelta(context.Background(), statuspkg.ChangedSinceFilter{
		ScopeID:           "does-not-exist",
		SinceGenerationID: "gen-x",
		SampleLimit:       25,
	})
	if err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	if summary.ScopeID != "" {
		t.Fatalf("ScopeID = %q, want empty for unknown scope", summary.ScopeID)
	}
}

func TestComputeChangedSinceDeltaScopeSelectorReturnsResolvedRepository(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: scopeRow("git-repository-scope:opaque", "repository", "gen-current", observed, false, "repository:r_b")},
		{rows: priorRow("gen-prior", observed.Add(-time.Hour))},
		{rows: [][]any{}},
	}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeChangedSinceDelta(context.Background(), statuspkg.ChangedSinceFilter{
		ScopeID:           "git-repository-scope:opaque",
		SinceGenerationID: "gen-prior",
		SampleLimit:       25,
	})
	if err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	if got, want := summary.Repository, "repository:r_b"; got != want {
		t.Fatalf("Repository = %q, want %q", got, want)
	}
}

func TestComputeChangedSinceDeltaDoesNotLabelNonRepositoryScopeAsRepository(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: scopeRow("service-scope:checkout", "service", "gen-current", observed, false, "service:checkout")},
		{rows: priorRow("gen-prior", observed.Add(-time.Hour))},
		{rows: [][]any{}},
	}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeChangedSinceDelta(context.Background(), statuspkg.ChangedSinceFilter{
		ScopeID:           "service-scope:checkout",
		SinceGenerationID: "gen-prior",
		SampleLimit:       25,
	})
	if err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	if summary.Repository != "" {
		t.Fatalf("Repository = %q, want empty for non-repository scope", summary.Repository)
	}
}

func TestComputeChangedSinceDeltaUnknownSinceGenerationReturnsNoPrior(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 13, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: scopeRow("git-repository-scope:acme/app", "repository", "gen-current", observed, false)},
		{}, // prior generation resolves to nothing
		{}, // retention event lookup also resolves to nothing
	}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeChangedSinceDelta(context.Background(), statuspkg.ChangedSinceFilter{
		ScopeID:           "git-repository-scope:acme/app",
		SinceGenerationID: "missing-gen",
		SampleLimit:       25,
	})
	if err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	if summary.ScopeID == "" {
		t.Fatalf("ScopeID empty, want resolved scope")
	}
	if summary.SinceGenerationID != "" {
		t.Fatalf("SinceGenerationID = %q, want empty when since reference unresolved", summary.SinceGenerationID)
	}
}

func TestComputeChangedSinceDeltaRetentionExpiredPriorIsUnavailable(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: scopeRow("git-repository-scope:acme/app", "repository", "gen-current", observed, false)},
		{}, // prior generation was pruned from scope_generations
		{rows: [][]any{{true, observed.Add(-8 * 24 * time.Hour)}}},
	}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeChangedSinceDelta(context.Background(), statuspkg.ChangedSinceFilter{
		ScopeID:           "git-repository-scope:acme/app",
		SinceGenerationID: "gen-pruned",
		SampleLimit:       25,
	})
	if err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	if !summary.Unavailable {
		t.Fatalf("Unavailable = false, want true for retention-expired prior")
	}
	if summary.UnavailableReason != statuspkg.ChangedSinceUnavailableRetentionExpired {
		t.Fatalf("UnavailableReason = %q, want %q", summary.UnavailableReason, statuspkg.ChangedSinceUnavailableRetentionExpired)
	}
	if summary.SinceGenerationID != "gen-pruned" {
		t.Fatalf("SinceGenerationID = %q, want requested pruned generation", summary.SinceGenerationID)
	}
	for _, category := range summary.Categories {
		if !category.Unavailable {
			t.Fatalf("category %s unavailable = false, want true", category.Category)
		}
	}
	if !strings.Contains(queryer.queries[2], "generation_retention_events") {
		t.Fatalf("retention-expired lookup query missing retention event table:\n%s", queryer.queries[2])
	}
}

func TestComputeChangedSinceDeltaNoActiveGenerationIsUnavailable(t *testing.T) {
	t.Parallel()

	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: scopeRow("git-repository-scope:acme/app", "repository", "", nil, true)},
	}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeChangedSinceDelta(context.Background(), statuspkg.ChangedSinceFilter{
		ScopeID:           "git-repository-scope:acme/app",
		SinceGenerationID: "gen-prior",
		SampleLimit:       25,
	})
	if err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	if !summary.Unavailable {
		t.Fatalf("Unavailable = false, want true when no active generation")
	}
	if !summary.Building {
		t.Fatalf("Building = false, want true when a pending generation is in flight")
	}
	for _, category := range summary.Categories {
		if !category.Unavailable {
			t.Fatalf("category %s should be unavailable", category.Category)
		}
	}
}

func TestComputeChangedSinceDeltaObservedAtResolution(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 14, 0, 0, 0, time.UTC)
	sinceAt := observed.Add(-30 * time.Minute)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: scopeRow("git-repository-scope:acme/app", "repository", "gen-current", observed, false)},
		{rows: priorRow("gen-prior", observed.Add(-time.Hour))},
		{rows: [][]any{diffRow("files", "unchanged", 1, "file:a", "file")}},
	}}
	store := NewStatusStore(queryer)

	summary, err := store.ComputeChangedSinceDelta(context.Background(), statuspkg.ChangedSinceFilter{
		ScopeID:         "git-repository-scope:acme/app",
		SinceObservedAt: sinceAt,
		SampleLimit:     25,
	})
	if err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	if summary.SinceObservedAt == "" {
		t.Fatalf("SinceObservedAt empty, want resolved prior observed_at")
	}
	// The generation resolution query carries the FULL OUTER JOIN-free baseline.
	if !strings.Contains(queryer.queries[1], "generation.observed_at <= $3") {
		t.Fatalf("prior generation query missing observed-at baseline:\n%s", queryer.queries[1])
	}
}

func TestComputeChangedSinceDeltaRequiresQueryer(t *testing.T) {
	t.Parallel()

	var store StatusStore
	if _, err := store.ComputeChangedSinceDelta(context.Background(), statuspkg.ChangedSinceFilter{}); err == nil {
		t.Fatal("expected error for nil queryer")
	}
}

func TestComputeChangedSinceDeltaDiffQueryUsesPayloadHashAndFullOuterJoin(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 15, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: scopeRow("s", "repository", "gen-current", observed, false)},
		{rows: priorRow("gen-prior", observed.Add(-time.Hour))},
		{rows: [][]any{}},
	}}
	store := NewStatusStore(queryer)

	if _, err := store.ComputeChangedSinceDelta(context.Background(), statuspkg.ChangedSinceFilter{
		ScopeID:           "s",
		SinceGenerationID: "gen-prior",
		SampleLimit:       25,
	}); err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	diffQuery := queryer.queries[2]
	for _, want := range []string{
		"sha256(convert_to((" + changedSincePayloadDigestInput + ")::text, 'UTF8'))",
		changedSinceExcludeReducerDerivedKinds,
		"FULL OUTER JOIN",
		"is_tombstone = TRUE",
		"GROUP BY fact_category, classification",
		"LEFT JOIN LATERAL",
	} {
		if !strings.Contains(diffQuery, want) {
			t.Fatalf("diff query missing %q:\n%s", want, diffQuery)
		}
	}
}

func TestComputeChangedSinceDeltaBucketWithoutSampleKeepsCount(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 16, 0, 0, 0, time.UTC)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: scopeRow("s", "repository", "gen-current", observed, false)},
		{rows: priorRow("gen-prior", observed.Add(-time.Hour))},
		{rows: [][]any{diffBucketOnlyRow("files", "added", 4)}},
	}}
	summary, err := NewStatusStore(queryer).ComputeChangedSinceDelta(context.Background(), statuspkg.ChangedSinceFilter{
		ScopeID:           "s",
		SinceGenerationID: "gen-prior",
		SampleLimit:       25,
	})
	if err != nil {
		t.Fatalf("ComputeChangedSinceDelta() error = %v", err)
	}
	files := summary.Categories[0]
	if files.Counts.Added != 4 {
		t.Fatalf("added count = %d, want 4", files.Counts.Added)
	}
	if len(files.Samples) != 0 || len(files.Truncated) != 0 {
		t.Fatalf("bucket without a sample key produced samples %v truncated %v", files.Samples, files.Truncated)
	}
}
