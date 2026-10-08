// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope/selection"
)

var (
	cycleOne     = time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC)
	testSelector = NewGitHubOrgSelector("githubOrg", "acme", nil, false, GitHubAppPrincipal("1", "2"))
	testWindow   = 48 * time.Hour
)

func scopeIDFor(slug string) string { return "git-repository-scope:" + slug }

// qaFixture mirrors the QA corpus behind #7625: 802 known acme scopes,
// of which 776 are listed and selectable (one under a renamed repository's
// new name), 24 were transferred out of the org, archived-service is listed
// but archived, and renamed-service is the renamed repository's old
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
		slug := fmt.Sprintf("acme/repo-%03d", i)
		addKnown(slug)
		addListed(slug, int64(10_000+i), StateSelected)
	}
	addKnown("acme/renamed-service-v2")
	addListed("acme/renamed-service-v2", 20_000, StateSelected)
	for i := range 24 {
		addKnown(fmt.Sprintf("acme/transferred-%02d", i))
	}
	addKnown("acme/archived-service")
	addListed("acme/archived-service", 30_000, StateArchivedExcluded)
	addKnown("acme/renamed-service")
	addListed("acme/never-indexed-a", 40_000, StateSelected)
	addListed("acme/never-indexed-b", 40_001, StateSelected)
	return known, listing
}

func evaluateAt(now time.Time, known []KnownScope, listing Listing, prior []Observation) Result {
	return Evaluate(Input{Selector: testSelector, Now: now, LivenessWindow: testWindow, Listing: listing, Known: known, Prior: prior})
}

func TestEvaluateQAFixtureConfirmsOnlyOnTheSecondEvaluation(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	if len(known) != 802 || len(listing.Repositories) != 779 {
		t.Fatalf("fixture = %d known, %d listed; want 802 and 779", len(known), len(listing.Repositories))
	}
	first := evaluateAt(cycleOne, known, listing, nil)
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
	if first.Batch.LivenessWindow != testWindow || !first.PreviousEvaluatedAt.IsZero() {
		t.Fatalf("cycle 1 window %v previous %v, want %v and no previous evaluation", first.Batch.LivenessWindow, first.PreviousEvaluatedAt, testWindow)
	}
	for _, observation := range first.Projected {
		if Confirmed(observation) {
			t.Fatalf("first evaluation confirmed %+v; a first evaluation confirms nothing", observation)
		}
	}
	assertRowState(t, first, "acme/renamed-service", StateNotListed)
	assertRowState(t, first, "acme/renamed-service-v2", StateSelected)
	assertRowState(t, first, "acme/archived-service", StateArchivedExcluded)
	if got := len(first.NotListedSample); got != 10 || !slices.IsSorted(first.NotListedSample) {
		t.Fatalf("not_listed_sample = %v, want 10 sorted slugs", first.NotListedSample)
	}

	second := evaluateAt(cycleOne.Add(selection.ConfirmationMinSpan), known, listing, first.Projected)
	if want := (ScopeCounts{Selected: 776, NotListed: 25, ArchivedExcluded: 1}); second.Scopes != want {
		t.Fatalf("cycle 2 scopes = %+v, want %+v", second.Scopes, want)
	}
	if second.Counts.NewlyUnlisted != 0 || !second.PreviousEvaluatedAt.Equal(cycleOne) {
		t.Fatalf("cycle 2 newly unlisted %d previous %v, want 0 and %v", second.Counts.NewlyUnlisted, second.PreviousEvaluatedAt, cycleOne)
	}
	for _, observation := range second.Projected {
		if observation.State != StateSelected && !Confirmed(observation) {
			t.Fatalf("cycle 2 %s %s is unconfirmed: %+v", observation.State, observation.ScopeID, observation)
		}
		if observation.StateCycleCount != 2 || !observation.StateSince.Equal(cycleOne) {
			t.Fatalf("cycle 2 %s = %+v, want two cycles since %v", observation.ScopeID, observation, cycleOne)
		}
	}
}

func TestEvaluateTwoQuickEvaluationsDoNotConfirm(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	first := evaluateAt(cycleOne, known, listing, nil)
	second := evaluateAt(cycleOne.Add(time.Minute), known, listing, first.Projected)
	if second.Scopes.NotListed != 0 || second.Scopes.NotListedPending != 25 {
		t.Fatalf("scopes one minute later = %+v, want 25 still pending", second.Scopes)
	}
	third := evaluateAt(cycleOne.Add(selection.ConfirmationMinSpan), known, listing, second.Projected)
	if third.Scopes.NotListed != 25 {
		t.Fatalf("scopes at the minimum span = %+v, want 25 confirmed", third.Scopes)
	}
}

// TestEvaluateAfterDowntimeConfirmsWithoutInflatingTheWindow is the
// amendment's downtime case: a not_listed row written before a 50h outage is
// confirmed by the first evaluation after it, the stored window stays 48h,
// and the 50h gap is reported for the lapse warning.
func TestEvaluateAfterDowntimeConfirmsWithoutInflatingTheWindow(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	before := evaluateAt(cycleOne, known, listing, nil)
	after := evaluateAt(cycleOne.Add(50*time.Hour), known, listing, before.Projected)
	if after.Batch.LivenessWindow != testWindow {
		t.Fatalf("window after a 50h gap = %v, want it unchanged at %v", after.Batch.LivenessWindow, testWindow)
	}
	if !after.PreviousEvaluatedAt.Equal(cycleOne) {
		t.Fatalf("previous evaluation = %v, want %v", after.PreviousEvaluatedAt, cycleOne)
	}
	if after.Scopes.NotListed != 25 || after.Scopes.NotListedPending != 0 {
		t.Fatalf("scopes after the outage = %+v, want the 25 pre-downtime misses confirmed", after.Scopes)
	}
}

func TestEvaluateDefaultsTheLivenessWindow(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	result := Evaluate(Input{Selector: testSelector, Now: cycleOne, Listing: listing, Known: known})
	if result.Batch.LivenessWindow != DefaultLivenessWindow || DefaultLivenessWindow != 48*time.Hour {
		t.Fatalf("window without config = %v, want the 48h default", result.Batch.LivenessWindow)
	}
}

func TestEvaluateStateChangeResetsAndRepeatIncrements(t *testing.T) {
	t.Parallel()

	since := cycleOne.Add(-72 * time.Hour)
	prior := func(slug string, state State, cycles int) Observation {
		return Observation{
			ScopeID: scopeIDFor(slug), State: state, LastListedAt: cycleOne.Add(-time.Hour),
			StateSince: since, StateCycleCount: cycles, EvaluatedAt: cycleOne.Add(-time.Hour), LivenessWindow: testWindow,
		}
	}
	listed := func(slug string, id int64, state State) ListedRepository {
		return ListedRepository{ScopeID: scopeIDFor(slug), Slug: slug, GitHubID: id, State: state}
	}
	known := []KnownScope{
		{ScopeID: scopeIDFor("acme/back"), Slug: "acme/back"},
		{ScopeID: scopeIDFor("acme/flip"), Slug: "acme/flip"},
		{ScopeID: scopeIDFor("acme/same"), Slug: "acme/same"},
		{ScopeID: scopeIDFor("acme/gone"), Slug: "acme/gone"},
	}
	result := evaluateAt(cycleOne, known, Listing{Complete: true, Repositories: []ListedRepository{
		listed("acme/back", 7, StateSelected),
		listed("acme/flip", 8, StateRuleExcluded),
		listed("acme/same", 9, StateSelected),
	}}, []Observation{
		prior("acme/back", StateNotListed, 3),
		prior("acme/flip", StateArchivedExcluded, 2),
		prior("acme/same", StateSelected, 4),
		prior("acme/gone", StateNotListed, 5),
	})
	if result.Counts.Relisted != 1 {
		t.Fatalf("relisted = %d, want 1", result.Counts.Relisted)
	}
	got := map[string]Observation{}
	for _, observation := range result.Projected {
		got[observation.ScopeID] = observation
	}
	for slug, want := range map[string]struct {
		state  State
		since  time.Time
		cycles int
	}{
		"acme/back": {StateSelected, cycleOne, 1},
		"acme/flip": {StateRuleExcluded, cycleOne, 1},
		"acme/same": {StateSelected, since, 5},
		"acme/gone": {StateNotListed, since, 6},
	} {
		o := got[scopeIDFor(slug)]
		if o.State != want.state || !o.StateSince.Equal(want.since) || o.StateCycleCount != want.cycles {
			t.Fatalf("%s = %+v, want %s since %v for %d cycles", slug, o, want.state, want.since, want.cycles)
		}
	}
	if back := got[scopeIDFor("acme/back")]; !back.LastListedAt.Equal(cycleOne) || back.GitHubRepoID != 7 {
		t.Fatalf("relisted projection = %+v, want last listed now with GitHub id 7", back)
	}
	if gone := got[scopeIDFor("acme/gone")]; !gone.LastListedAt.Equal(cycleOne.Add(-time.Hour)) {
		t.Fatalf("still-unlisted projection = %+v, want last_listed_at kept", gone)
	}
}

func TestEvaluateReplayAtTheSameTimeLeavesTheProjectionUnchanged(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	first := evaluateAt(cycleOne, known, listing, nil)
	replay := evaluateAt(cycleOne, known, listing, first.Projected)
	if !slices.Equal(replay.Projected, first.Projected) {
		t.Fatal("replaying the same evaluated_at changed the projected observations")
	}
	if replay.Scopes != first.Scopes {
		t.Fatalf("replay scopes = %+v, want %+v", replay.Scopes, first.Scopes)
	}
}

func TestEvaluateIgnoresScopesOutsideTheSelectorOrg(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	known = append(known, KnownScope{ScopeID: scopeIDFor("acme-archive/transferred-00"), Slug: "acme-archive/transferred-00"})
	result := evaluateAt(cycleOne, known, listing, nil)
	for _, row := range result.Batch.Rows {
		if row.ScopeID == scopeIDFor("acme-archive/transferred-00") {
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
		if listing.Repositories[i].Slug == "acme/repo-000" {
			listing.Repositories[i].State = StateNotListed
		}
	}
	result := evaluateAt(cycleOne, known, listing, nil)
	if result.Counts.Listed != 778 || result.Counts.NewlyUnlisted != 26 {
		t.Fatalf("counts = %+v, want 778 listed and 26 newly unlisted (the bogus record ignored)", result.Counts)
	}
}

func TestEvaluateTruncatedListingWritesNothing(t *testing.T) {
	t.Parallel()

	known, listing := qaFixture()
	listing.Complete = false
	result := evaluateAt(cycleOne, known, listing, nil)
	if result.Outcome != OutcomeListingTruncated || len(result.Batch.Rows) != 0 {
		t.Fatalf("truncated result = %q with %d rows, want %q with none", result.Outcome, len(result.Batch.Rows), OutcomeListingTruncated)
	}
}

func TestEvaluateMassMissGuardWritesNothing(t *testing.T) {
	t.Parallel()

	known := make([]KnownScope, 0, 100)
	listing := Listing{Complete: true}
	for i := range 100 {
		slug := fmt.Sprintf("acme/repo-%03d", i)
		known = append(known, KnownScope{ScopeID: scopeIDFor(slug), Slug: slug})
		if i >= 11 {
			listing.Repositories = append(listing.Repositories, ListedRepository{ScopeID: scopeIDFor(slug), Slug: slug, State: StateSelected})
		}
	}
	tripped := evaluateAt(cycleOne, known, listing, nil)
	if tripped.Outcome != OutcomeGuardTripped || len(tripped.Batch.Rows) != 0 {
		t.Fatalf("11 of 100 unlisted = %q with %d rows, want %q with none", tripped.Outcome, len(tripped.Batch.Rows), OutcomeGuardTripped)
	}
	if tripped.GuardThreshold != 10 || tripped.Counts.NewlyUnlisted != 11 {
		t.Fatalf("guard threshold %d newly %d, want 10 and 11", tripped.GuardThreshold, tripped.Counts.NewlyUnlisted)
	}

	listing.Repositories = append(listing.Repositories, ListedRepository{ScopeID: scopeIDFor("acme/repo-000"), Slug: "acme/repo-000", State: StateSelected})
	if passed := evaluateAt(cycleOne, known, listing, nil); passed.Outcome != OutcomeEvaluated {
		t.Fatalf("10 of 100 unlisted = %q, want %q", passed.Outcome, OutcomeEvaluated)
	}

	empty := evaluateAt(cycleOne, known[:3], Listing{Complete: true}, nil)
	if empty.Outcome != OutcomeGuardTripped || len(empty.Batch.Rows) != 0 {
		t.Fatalf("empty listing over 3 known = %q with %d rows, want %q with none", empty.Outcome, len(empty.Batch.Rows), OutcomeGuardTripped)
	}
	if none := evaluateAt(cycleOne, nil, Listing{Complete: true}, nil); none.Outcome != OutcomeEvaluated {
		t.Fatalf("empty listing with no known scopes = %q, want %q", none.Outcome, OutcomeEvaluated)
	}
}

func TestEvaluateGuardCountsOnlyNewlyUnlistedScopes(t *testing.T) {
	t.Parallel()

	known := make([]KnownScope, 0, 100)
	prior := make([]Observation, 0, 20)
	listing := Listing{Complete: true}
	for i := range 100 {
		slug := fmt.Sprintf("acme/repo-%03d", i)
		known = append(known, KnownScope{ScopeID: scopeIDFor(slug), Slug: slug})
		if i < 20 {
			prior = append(prior, Observation{
				ScopeID: scopeIDFor(slug), State: StateNotListed, StateCycleCount: 1,
				StateSince: cycleOne.Add(-time.Hour), EvaluatedAt: cycleOne.Add(-time.Hour), LivenessWindow: testWindow,
			})
			continue
		}
		listing.Repositories = append(listing.Repositories, ListedRepository{ScopeID: scopeIDFor(slug), Slug: slug, State: StateSelected})
	}
	result := evaluateAt(cycleOne, known, listing, prior)
	if result.Outcome != OutcomeEvaluated || result.Counts.NewlyUnlisted != 0 || result.Counts.NotListed != 20 {
		t.Fatalf("20 already-unlisted scopes = %q, counts %+v; want evaluated with 0 newly unlisted", result.Outcome, result.Counts)
	}
}

// TestEvaluateExplicitWritesOnlySelectedRows covers explicit mode: rows only
// for configured repositories that already have a scope, every row selected,
// no not_listed rows for other known scopes, and no mass-miss guard.
func TestEvaluateExplicitWritesOnlySelectedRows(t *testing.T) {
	t.Parallel()

	selector := NewExplicitSelector("explicit", "acme", []Rule{{Kind: "exact", Value: "acme/repo-001"}}, TokenPrincipal("t"))
	known, _ := qaFixture()
	listing := Listing{Complete: true, Repositories: []ListedRepository{
		{ScopeID: scopeIDFor("acme/repo-001"), Slug: "acme/repo-001", State: StateSelected},
		{ScopeID: scopeIDFor("acme/repo-002"), Slug: "acme/repo-002", State: StateSelected},
		{ScopeID: scopeIDFor("acme/never-indexed"), Slug: "acme/never-indexed", State: StateSelected},
	}}
	result := Evaluate(Input{Selector: selector, Now: cycleOne, LivenessWindow: testWindow, Listing: listing, Known: known})
	if result.Outcome != OutcomeEvaluated {
		t.Fatalf("explicit outcome = %q, want %q", result.Outcome, OutcomeEvaluated)
	}
	want := []Row{
		{ScopeID: scopeIDFor("acme/repo-001"), State: StateSelected},
		{ScopeID: scopeIDFor("acme/repo-002"), State: StateSelected},
	}
	if !slices.Equal(result.Batch.Rows, want) {
		t.Fatalf("explicit rows = %+v, want %+v", result.Batch.Rows, want)
	}
	if result.Counts.NotListed != 0 || result.Counts.NewlyUnlisted != 0 || len(result.NotListedSample) != 0 {
		t.Fatalf("explicit counts = %+v, want no unlisted scopes", result.Counts)
	}

	empty := Evaluate(Input{Selector: selector, Now: cycleOne, LivenessWindow: testWindow, Listing: Listing{Complete: true}, Known: known})
	if empty.Outcome != OutcomeEvaluated || len(empty.Batch.Rows) != 0 {
		t.Fatalf("explicit with nothing configured = %q with %d rows, want evaluated with none (no guard)", empty.Outcome, len(empty.Batch.Rows))
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
