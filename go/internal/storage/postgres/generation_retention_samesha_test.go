// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
	"time"
)

// testSameSHAClock returns a fixed clock far from any retention boundary so
// the same-identity unit tests never depend on the wall clock.
func testSameSHAClock() time.Time {
	return time.Date(2026, time.October, 12, 12, 0, 0, 0, time.UTC)
}

// The general candidate query carries the #7981 same-identity dupe leg, so
// every marker below is present. The targeted re-lock query must carry the
// same leg so the two stay in lockstep; the shared live test pins their
// behavior together.
func TestGenerationRetentionSameSHADupeLegShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"source_commit_sha",
		"COALESCE(newer.freshness_hint, '')",
		"newer.generation_id = scope.active_generation_id",
		"superseded_rank > $2 OR",
	} {
		if !strings.Contains(generationRetentionCandidateQuery, want) {
			t.Errorf("general candidate query missing same-SHA marker %q", want)
		}
	}
}

func TestGenerationRetentionTargetedSameSHADupeLegShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"source_commit_sha",
		"COALESCE(newer.freshness_hint, '')",
		"newer.generation_id = scope.active_generation_id",
	} {
		if !strings.Contains(generationRetentionTargetedCandidateQuery, want) {
			t.Errorf("targeted candidate query missing same-SHA marker %q", want)
		}
	}
}

// TestGenerationRetentionWitnessRetainsNewestCopyPerIdentity pins the #7981
// Go-side witness: per (scope, non-empty SHA, is_delta, freshness hint)
// group the newest copy sits the batch out while older copies prune.
// Singleton groups, empty-SHA members, other scopes, deltas and hints pass
// through; the newest order is superseded_at with a generation_id tie-break,
// the same total order the SQL witness compares as a row tuple.
func TestGenerationRetentionWitnessRetainsNewestCopyPerIdentity(t *testing.T) {
	t.Parallel()

	now := testSameSHAClock()
	identity := func(gen string, age time.Duration) generationRetentionCandidate {
		return generationRetentionCandidate{
			scopeID:         "scope-a",
			generationID:    gen,
			scopeKind:       "repository",
			supersededAt:    now.Add(-age),
			observedAt:      now.Add(-age),
			sourceCommitSHA: "sha-a",
			freshnessHint:   "hint-a",
		}
	}
	candidates := []generationRetentionCandidate{
		identity("gen-old", 30*24*time.Hour),
		identity("gen-mid", 20*24*time.Hour),
		identity("gen-new", 10*24*time.Hour),
		{
			scopeID: "scope-a", generationID: "gen-singleton", scopeKind: "repository",
			supersededAt: now.Add(-5 * 24 * time.Hour), observedAt: now.Add(-5 * 24 * time.Hour),
			sourceCommitSHA: "sha-alone",
		},
		{
			scopeID: "scope-a", generationID: "gen-empty-sha", scopeKind: "repository",
			supersededAt: now.Add(-40 * 24 * time.Hour), observedAt: now.Add(-40 * 24 * time.Hour),
		},
		{
			scopeID: "scope-b", generationID: "gen-other-scope", scopeKind: "repository",
			supersededAt: now.Add(-30 * 24 * time.Hour), observedAt: now.Add(-30 * 24 * time.Hour),
			sourceCommitSHA: "sha-a", freshnessHint: "hint-a",
		},
		{
			scopeID: "scope-a", generationID: "gen-delta", scopeKind: "repository",
			supersededAt: now.Add(-30 * 24 * time.Hour), observedAt: now.Add(-30 * 24 * time.Hour),
			sourceCommitSHA: "sha-a", isDelta: true, freshnessHint: "hint-a",
		},
		{
			scopeID: "scope-a", generationID: "gen-hint", scopeKind: "repository",
			supersededAt: now.Add(-30 * 24 * time.Hour), observedAt: now.Add(-30 * 24 * time.Hour),
			sourceCommitSHA: "sha-a", freshnessHint: "hint-b",
		},
	}

	prunable, retained := dropNewestSameIdentityPerBatch(candidates)
	if len(retained) != 1 || retained[0].generationID != "gen-new" {
		t.Fatalf("retained = %v, want only gen-new (the newest copy of its identity group)", generationIDsOf(retained))
	}
	if len(prunable) != len(candidates)-1 {
		t.Fatalf("prunable = %d, want %d", len(prunable), len(candidates)-1)
	}

	// A generation_id tie-break decides newest when superseded_at ties.
	tied := []generationRetentionCandidate{
		{
			scopeID: "scope-a", generationID: "gen-a", scopeKind: "repository",
			supersededAt: now, observedAt: now, sourceCommitSHA: "sha-t",
		},
		{
			scopeID: "scope-a", generationID: "gen-b", scopeKind: "repository",
			supersededAt: now, observedAt: now, sourceCommitSHA: "sha-t",
		},
	}
	if _, retained := dropNewestSameIdentityPerBatch(tied); len(retained) != 1 || retained[0].generationID != "gen-b" {
		t.Fatalf("tied retained = %v, want only gen-b (generation_id tie-break)", generationIDsOf(retained))
	}
}

func generationIDsOf(candidates []generationRetentionCandidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.generationID)
	}
	return ids
}

// TestGenerationRetentionCandidateScanCarriesIdentity pins the #7981 scan
// contract: scanRetentionCandidates returns the same-identity columns the
// witness rule groups by (source_commit_sha, is_delta, freshness_hint), in
// scan order, while six-column rows from older fixtures keep scanning with
// zero identity values.
func TestGenerationRetentionCandidateScanCarriesIdentity(t *testing.T) {
	t.Parallel()

	now := testSameSHAClock()
	full := []any{"scope-a", "gen-a", "repository", now, now, false, "sha-a", true, "hint-a"}
	got, err := scanRetentionCandidates(&queueFakeRows{rows: [][]any{full}})
	if err != nil {
		t.Fatalf("scan nine-column row: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	candidate := got[0]
	if candidate.sourceCommitSHA != "sha-a" || !candidate.isDelta || candidate.freshnessHint != "hint-a" {
		t.Fatalf("identity = (%q, %v, %q), want (sha-a, true, hint-a)",
			candidate.sourceCommitSHA, candidate.isDelta, candidate.freshnessHint)
	}

	legacy := []any{"scope-a", "gen-a", "repository", now, now, false}
	got, err = scanRetentionCandidates(&queueFakeRows{rows: [][]any{legacy}})
	if err != nil {
		t.Fatalf("scan six-column row: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidates = %d, want 1", len(got))
	}
	if legacy := got[0]; legacy.sourceCommitSHA != "" || legacy.isDelta || legacy.freshnessHint != "" {
		t.Fatalf("legacy identity = (%q, %v, %q), want zero values",
			legacy.sourceCommitSHA, legacy.isDelta, legacy.freshnessHint)
	}
}
