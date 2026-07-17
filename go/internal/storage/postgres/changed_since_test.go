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

func countRow(category, classification string, count int64) []any {
	return []any{category, classification, count}
}

func TestComputeChangedSinceDeltaUnchangedProducesNoFalseDeltas(t *testing.T) {
	t.Parallel()

	observed := time.Date(2026, 6, 9, 10, 0, 0, 0, time.UTC)
	prior := observed.Add(-time.Hour)
	queryer := &fakeQueryer{responses: []fakeRows{
		{rows: scopeRow("git-repository-scope:acme/app", "repository", "gen-current", observed, false)},
		{rows: priorRow("gen-prior", prior)},
		{rows: [][]any{
			countRow("files", "unchanged", 12),
			countRow("content_entities", "unchanged", 8),
			countRow("facts", "unchanged", 4),
		}},
		// Sample reads for the three unchanged buckets (counts > 0).
		{rows: [][]any{{"file:a", "file"}}},
		{rows: [][]any{{"entity:a", "content_entity"}}},
		{rows: [][]any{{"fact:a", "aws_resource"}}},
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
			countRow("files", "added", 2),
			countRow("files", "updated", 1),
			countRow("files", "retired", 1),
			countRow("files", "superseded", 1),
			countRow("files", "unchanged", 3),
		}},
		// One sample read per non-zero files classification, ordered as in
		// ChangedSinceClassifications: added, updated, unchanged, retired, superseded.
		{rows: [][]any{{"file:new1", "file"}, {"file:new2", "file"}}},
		{rows: [][]any{{"file:upd", "file"}}},
		{rows: [][]any{{"file:same", "file"}}},
		{rows: [][]any{{"file:gone", "file"}}},
		{rows: [][]any{{"file:dropped", "file"}}},
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
		{rows: [][]any{countRow("facts", "added", 5)}},
		{rows: [][]any{{"fact:a", "k"}, {"fact:b", "k"}, {"fact:c", "k"}}},
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
		{rows: [][]any{countRow("files", "unchanged", 1)}},
		{rows: [][]any{{"file:a", "file"}}},
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

func TestComputeChangedSinceDeltaCountsQueryUsesPayloadHashAndFullOuterJoin(t *testing.T) {
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
	countsQuery := queryer.queries[2]
	for _, want := range []string{
		"md5(payload::text)",
		"FULL OUTER JOIN",
		"is_tombstone = TRUE",
		"GROUP BY fact_category, classification",
	} {
		if !strings.Contains(countsQuery, want) {
			t.Fatalf("counts query missing %q:\n%s", want, countsQuery)
		}
	}
}
