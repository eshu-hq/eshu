// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

var (
	cycleOne        = time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)
	testSelector    = NewGitHubOrgSelector("githubOrg", "boatsgroup", nil, false)
	minimumInterval = 5 * time.Minute
)

func scopeIDFor(slug string) string { return "git-repository-scope:" + slug }

// qaFixture mirrors the QA corpus behind #7625: 802 known boatsgroup scopes,
// of which 776 are listed and selectable (one under a renamed repository's
// new name), 24 were transferred out of the org, fsbo-hapi-patched is listed
// but archived, and script-node-bulk-feed is the renamed repository's old
// name. The listing also carries two selectable repositories that were never
// indexed and so have no scope.
func qaFixture() ([]KnownScope, Listing) {
	known := make([]KnownScope, 0, 802)
	listing := Listing{Complete: true}
	addKnown := func(slug string) { known = append(known, KnownScope{ScopeID: scopeIDFor(slug), Slug: slug}) }
	addListed := func(slug string, id int64, state State) {
		listing.Repositories = append(listing.Repositories, ListedRepository{
			ScopeID: scopeIDFor(slug), Slug: slug, GitHubID: id, State: state,
		})
	}
	for i := range 775 {
		slug := fmt.Sprintf("boatsgroup/repo-%03d", i)
		addKnown(slug)
		addListed(slug, int64(10_000+i), StateSelected)
	}
	addKnown("boatsgroup/script-node-bulk-feed-v2")
	addListed("boatsgroup/script-node-bulk-feed-v2", 20_000, StateSelected)
	for i := range 24 {
		addKnown(fmt.Sprintf("boatsgroup/transferred-%02d", i))
	}
	addKnown("boatsgroup/fsbo-hapi-patched")
	addListed("boatsgroup/fsbo-hapi-patched", 30_000, StateArchivedExcluded)
	addKnown("boatsgroup/script-node-bulk-feed")
	addListed("boatsgroup/never-indexed-a", 40_000, StateSelected)
	addListed("boatsgroup/never-indexed-b", 40_001, StateSelected)
	return known, listing
}

func TestEvaluateQAFixtureConfirmsDroppedReposOnlyAfterTwoCycles(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	if len(known) != 802 || len(listing.Repositories) != 779 {
		t.Fatalf("fixture = %d known, %d listed; want 802 and 779", len(known), len(listing.Repositories))
	}
	first := Evaluate(Input{
		Selector: testSelector, Now: cycleOne, MinimumInterval: minimumInterval,
		Listing: listing, Known: known,
	})
	if first.Outcome != OutcomeEvaluated {
		t.Fatalf("cycle 1 outcome = %q, want %q", first.Outcome, OutcomeEvaluated)
	}
	if got, want := len(first.Batch.Rows), 802; got != want {
		t.Fatalf("cycle 1 rows = %d, want %d (one per known scope, none for never-indexed repos)", got, want)
	}
	if want := (ScopeCounts{Selected: 776, NotListedPending: 25, ArchivedExcluded: 1}); first.Scopes != want {
		t.Fatalf("cycle 1 scopes = %+v, want %+v", first.Scopes, want)
	}
	if first.Counts.NewlyUnlisted != 25 || first.Counts.NotListed != 25 || first.Counts.Relisted != 0 {
		t.Fatalf("cycle 1 counts = %+v, want 25 newly unlisted, 25 not listed, 0 relisted", first.Counts)
	}
	if first.Counts.Listed != 779 || first.Counts.Selectable != 778 || first.Counts.ArchivedExcluded != 1 || first.Counts.Known != 802 {
		t.Fatalf("cycle 1 listing counts = %+v", first.Counts)
	}
	if first.Batch.Interval != minimumInterval {
		t.Fatalf("cycle 1 interval = %v, want the %v floor with no prior evaluation", first.Batch.Interval, minimumInterval)
	}
	assertRowState(t, first, "boatsgroup/script-node-bulk-feed", StateNotListed)
	assertRowState(t, first, "boatsgroup/script-node-bulk-feed-v2", StateSelected)
	assertRowState(t, first, "boatsgroup/fsbo-hapi-patched", StateArchivedExcluded)
	if got := len(first.NotListedSample); got != 10 || !slices.IsSorted(first.NotListedSample) {
		t.Fatalf("not_listed_sample = %v, want 10 sorted slugs", first.NotListedSample)
	}

	second := Evaluate(Input{
		Selector: testSelector, Now: cycleOne.Add(minimumInterval), MinimumInterval: minimumInterval,
		Listing: listing, Known: known, Prior: first.Projected,
	})
	if second.Outcome != OutcomeEvaluated {
		t.Fatalf("cycle 2 outcome = %q, want %q", second.Outcome, OutcomeEvaluated)
	}
	if want := (ScopeCounts{Selected: 776, NotListed: 25, ArchivedExcluded: 1}); second.Scopes != want {
		t.Fatalf("cycle 2 scopes = %+v, want %+v", second.Scopes, want)
	}
	if second.Counts.NewlyUnlisted != 0 {
		t.Fatalf("cycle 2 newly unlisted = %d, want 0", second.Counts.NewlyUnlisted)
	}
	for _, observation := range second.Projected {
		if observation.State == StateNotListed && !Confirmed(observation) {
			t.Fatalf("cycle 2 not_listed %s is unconfirmed: %+v", observation.ScopeID, observation)
		}
		if observation.State != StateNotListed && Confirmed(observation) {
			t.Fatalf("cycle 2 %s scope %s reports confirmed", observation.State, observation.ScopeID)
		}
	}
}

func TestEvaluateTwoQuickCyclesDoNotConfirmBeforeTheInterval(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	first := Evaluate(Input{Selector: testSelector, Now: cycleOne, MinimumInterval: minimumInterval, Listing: listing, Known: known})
	second := Evaluate(Input{
		Selector: testSelector, Now: cycleOne.Add(time.Minute), MinimumInterval: minimumInterval,
		Listing: listing, Known: known, Prior: first.Projected,
	})
	if second.Scopes.NotListed != 0 || second.Scopes.NotListedPending != 25 {
		t.Fatalf("scopes one minute later = %+v, want 25 still pending", second.Scopes)
	}
	third := Evaluate(Input{
		Selector: testSelector, Now: cycleOne.Add(minimumInterval), MinimumInterval: minimumInterval,
		Listing: listing, Known: known, Prior: second.Projected,
	})
	if third.Scopes.NotListed != 25 {
		t.Fatalf("scopes at the interval = %+v, want 25 confirmed", third.Scopes)
	}
}

func TestEvaluateIntervalTracksTheGapSincePriorEvaluation(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	first := Evaluate(Input{Selector: testSelector, Now: cycleOne, MinimumInterval: minimumInterval, Listing: listing, Known: known})
	later := Evaluate(Input{
		Selector: testSelector, Now: cycleOne.Add(40 * time.Minute), MinimumInterval: minimumInterval,
		Listing: listing, Known: known, Prior: first.Projected,
	})
	if later.Batch.Interval != 40*time.Minute {
		t.Fatalf("interval = %v, want the 40m gap since the prior evaluation", later.Batch.Interval)
	}
	if defaulted := Evaluate(Input{Selector: testSelector, Now: cycleOne, Listing: listing, Known: known}); defaulted.Batch.Interval != DefaultMinimumInterval {
		t.Fatalf("interval without a floor = %v, want %v", defaulted.Batch.Interval, DefaultMinimumInterval)
	}
}

func TestEvaluateIgnoresScopesOutsideTheSelectorOrg(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	known = append(known, KnownScope{ScopeID: scopeIDFor("boatsgroup-archive/transferred-00"), Slug: "boatsgroup-archive/transferred-00"})
	result := Evaluate(Input{Selector: testSelector, Now: cycleOne, MinimumInterval: minimumInterval, Listing: listing, Known: known})
	for _, row := range result.Batch.Rows {
		if row.ScopeID == scopeIDFor("boatsgroup-archive/transferred-00") {
			t.Fatalf("evaluator wrote a row for another org's scope: %+v", row)
		}
	}
	if result.Counts.Known != 802 {
		t.Fatalf("known = %d, want 802 (other-org scope excluded)", result.Counts.Known)
	}
}

func TestEvaluateIgnoresListedRecordsThatClaimNotListed(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	for i := range listing.Repositories {
		if listing.Repositories[i].Slug == "boatsgroup/repo-000" {
			listing.Repositories[i].State = StateNotListed
		}
	}
	result := Evaluate(Input{Selector: testSelector, Now: cycleOne, MinimumInterval: minimumInterval, Listing: listing, Known: known})
	if result.Counts.Listed != 778 || result.Counts.NewlyUnlisted != 26 {
		t.Fatalf("counts = %+v, want 778 listed and 26 newly unlisted (the bogus record ignored)", result.Counts)
	}
	for _, observation := range result.Projected {
		if observation.ScopeID == scopeIDFor("boatsgroup/repo-000") && observation.UnlistedCycleCount != 1 {
			t.Fatalf("repo-000 = %+v, want one unlisted cycle", observation)
		}
	}
}

func TestEvaluateTruncatedListingWritesNothing(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	listing.Complete = false
	result := Evaluate(Input{Selector: testSelector, Now: cycleOne, MinimumInterval: minimumInterval, Listing: listing, Known: known})
	if result.Outcome != OutcomeListingTruncated || len(result.Batch.Rows) != 0 {
		t.Fatalf("truncated result = %q with %d rows, want %q with none", result.Outcome, len(result.Batch.Rows), OutcomeListingTruncated)
	}
}

func TestEvaluateMassMissGuardWritesNothing(t *testing.T) {
	t.Parallel()

	known := make([]KnownScope, 0, 100)
	listing := Listing{Complete: true}
	for i := range 100 {
		slug := fmt.Sprintf("boatsgroup/repo-%03d", i)
		known = append(known, KnownScope{ScopeID: scopeIDFor(slug), Slug: slug})
		if i >= 11 {
			listing.Repositories = append(listing.Repositories, ListedRepository{ScopeID: scopeIDFor(slug), Slug: slug, State: StateSelected})
		}
	}
	tripped := Evaluate(Input{Selector: testSelector, Now: cycleOne, MinimumInterval: minimumInterval, Listing: listing, Known: known})
	if tripped.Outcome != OutcomeGuardTripped || len(tripped.Batch.Rows) != 0 {
		t.Fatalf("11 of 100 unlisted = %q with %d rows, want %q with none", tripped.Outcome, len(tripped.Batch.Rows), OutcomeGuardTripped)
	}
	if tripped.GuardThreshold != 10 || tripped.Counts.NewlyUnlisted != 11 {
		t.Fatalf("guard threshold %d newly %d, want 10 and 11", tripped.GuardThreshold, tripped.Counts.NewlyUnlisted)
	}

	listing.Repositories = append(listing.Repositories, ListedRepository{ScopeID: scopeIDFor("boatsgroup/repo-000"), Slug: "boatsgroup/repo-000", State: StateSelected})
	if passed := Evaluate(Input{Selector: testSelector, Now: cycleOne, MinimumInterval: minimumInterval, Listing: listing, Known: known}); passed.Outcome != OutcomeEvaluated {
		t.Fatalf("10 of 100 unlisted = %q, want %q", passed.Outcome, OutcomeEvaluated)
	}

	empty := Evaluate(Input{Selector: testSelector, Now: cycleOne, MinimumInterval: minimumInterval, Listing: Listing{Complete: true}, Known: known[:3]})
	if empty.Outcome != OutcomeGuardTripped || len(empty.Batch.Rows) != 0 {
		t.Fatalf("empty listing over 3 known = %q with %d rows, want %q with none", empty.Outcome, len(empty.Batch.Rows), OutcomeGuardTripped)
	}
	if none := Evaluate(Input{Selector: testSelector, Now: cycleOne, MinimumInterval: minimumInterval, Listing: Listing{Complete: true}}); none.Outcome != OutcomeEvaluated {
		t.Fatalf("empty listing with no known scopes = %q, want %q", none.Outcome, OutcomeEvaluated)
	}
}

func TestEvaluateGuardCountsOnlyNewlyUnlistedScopes(t *testing.T) {
	t.Parallel()

	known := make([]KnownScope, 0, 100)
	prior := make([]Observation, 0, 20)
	listing := Listing{Complete: true}
	for i := range 100 {
		slug := fmt.Sprintf("boatsgroup/repo-%03d", i)
		known = append(known, KnownScope{ScopeID: scopeIDFor(slug), Slug: slug})
		if i < 20 {
			prior = append(prior, Observation{
				ScopeID: scopeIDFor(slug), State: StateNotListed, UnlistedCycleCount: 1,
				FirstUnlistedAt: cycleOne.Add(-time.Hour), EvaluatedAt: cycleOne.Add(-time.Hour), EvaluationInterval: minimumInterval,
			})
			continue
		}
		listing.Repositories = append(listing.Repositories, ListedRepository{ScopeID: scopeIDFor(slug), Slug: slug, State: StateSelected})
	}
	result := Evaluate(Input{Selector: testSelector, Now: cycleOne, MinimumInterval: minimumInterval, Listing: listing, Known: known, Prior: prior})
	if result.Outcome != OutcomeEvaluated || result.Counts.NewlyUnlisted != 0 || result.Counts.NotListed != 20 {
		t.Fatalf("20 already-unlisted scopes = %q, counts %+v; want evaluated with 0 newly unlisted", result.Outcome, result.Counts)
	}
}

func TestEvaluateRelistedScopeResetsCounters(t *testing.T) {
	t.Parallel()

	slug := "boatsgroup/back"
	prior := []Observation{{
		ScopeID: scopeIDFor(slug), State: StateNotListed, UnlistedCycleCount: 3,
		FirstUnlistedAt: cycleOne.Add(-2 * time.Hour), EvaluatedAt: cycleOne.Add(-time.Hour), EvaluationInterval: minimumInterval,
	}}
	result := Evaluate(Input{
		Selector: testSelector, Now: cycleOne, MinimumInterval: minimumInterval,
		Listing: Listing{Complete: true, Repositories: []ListedRepository{{ScopeID: scopeIDFor(slug), Slug: slug, GitHubID: 7, State: StateSelected}}},
		Known:   []KnownScope{{ScopeID: scopeIDFor(slug), Slug: slug}},
		Prior:   prior,
	})
	if result.Counts.Relisted != 1 {
		t.Fatalf("relisted = %d, want 1", result.Counts.Relisted)
	}
	got := result.Projected[0]
	if got.State != StateSelected || got.UnlistedCycleCount != 0 || !got.FirstUnlistedAt.IsZero() || !got.LastListedAt.Equal(cycleOne) || got.GitHubRepoID != 7 {
		t.Fatalf("relisted projection = %+v, want selected with reset counters, last listed now, GitHub id 7", got)
	}
}

func TestEvaluateReplayAtTheSameTimeLeavesTheProjectionUnchanged(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	first := Evaluate(Input{Selector: testSelector, Now: cycleOne, MinimumInterval: minimumInterval, Listing: listing, Known: known})
	replay := Evaluate(Input{Selector: testSelector, Now: cycleOne, MinimumInterval: minimumInterval, Listing: listing, Known: known, Prior: first.Projected})
	if !slices.Equal(replay.Projected, first.Projected) {
		t.Fatal("replaying the same evaluated_at changed the projected observations")
	}
	if replay.Scopes != first.Scopes {
		t.Fatalf("replay scopes = %+v, want %+v", replay.Scopes, first.Scopes)
	}
}

func TestConfirmedNeedsTwoCyclesAndTheInterval(t *testing.T) {
	t.Parallel()

	base := Observation{
		State: StateNotListed, UnlistedCycleCount: 2,
		FirstUnlistedAt: cycleOne, EvaluatedAt: cycleOne.Add(minimumInterval), EvaluationInterval: minimumInterval,
	}
	if !Confirmed(base) {
		t.Fatalf("Confirmed(%+v) = false, want true", base)
	}
	oneCycle := base
	oneCycle.UnlistedCycleCount = 1
	oneCycle.FirstUnlistedAt = cycleOne.Add(-time.Hour)
	if Confirmed(oneCycle) {
		t.Fatalf("Confirmed(one cycle spanning an hour) = true, want false")
	}
	tooSoon := base
	tooSoon.EvaluatedAt = cycleOne.Add(minimumInterval - time.Second)
	if Confirmed(tooSoon) {
		t.Fatalf("Confirmed(two cycles inside the interval) = true, want false")
	}
	archived := base
	archived.State = StateArchivedExcluded
	if Confirmed(archived) {
		t.Fatalf("Confirmed(archived_excluded) = true, want false")
	}
}

func assertRowState(t *testing.T, result Result, slug string, want State) {
	t.Helper()
	for _, row := range result.Batch.Rows {
		if row.ScopeID == scopeIDFor(slug) {
			if row.State != want {
				t.Fatalf("row %s state = %q, want %q", slug, row.State, want)
			}
			return
		}
	}
	t.Fatalf("no row for %s", slug)
}
