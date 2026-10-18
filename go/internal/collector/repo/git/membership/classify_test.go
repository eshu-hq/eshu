// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"testing"
	"time"
)

func probeTarget(scope, slug string, id int64, since time.Time) ProbeTarget {
	return ProbeTarget{
		ScopeID: scope, Slug: slug, GitHubRepoID: id,
		SelectorOwner: "acme", StateSince: since,
	}
}

func TestClassifyTransferred(t *testing.T) {
	t.Parallel()

	got, successor, update := Classify(
		probeTarget("git-repository-scope:acme/old", "acme/old", 101, cycleOne),
		ProbeResult{Found: true, FullName: "archive/old"},
	)
	if !update || got != UpstreamTransferred || successor != "archive/old" {
		t.Fatalf("Classify() = (%q, %q, %v), want (transferred, archive/old, true)", got, successor, update)
	}
}

func TestClassifyRenamedWithSelectedSuccessor(t *testing.T) {
	t.Parallel()

	got, successor, update := Classify(
		probeTarget("git-repository-scope:acme/renamed-service", "acme/renamed-service", 202, cycleOne),
		ProbeResult{Found: true, FullName: "acme/renamed-service-v2"},
	)
	if !update || got != UpstreamRenamed || successor != "acme/renamed-service-v2" {
		t.Fatalf("Classify() = (%q, %q, %v), want (renamed, acme/renamed-service-v2, true)", got, successor, update)
	}
}

func TestClassifyRenamedOut(t *testing.T) {
	t.Parallel()

	// The successor is not a known scope. The probe still reports renamed;
	// successor resolution happens at read time only.
	got, successor, update := Classify(
		probeTarget("git-repository-scope:acme/gone", "acme/gone", 203, cycleOne),
		ProbeResult{Found: true, FullName: "acme/gone-elsewhere"},
	)
	if !update || got != UpstreamRenamed || successor != "acme/gone-elsewhere" {
		t.Fatalf("Classify() = (%q, %q, %v), want (renamed, acme/gone-elsewhere, true)", got, successor, update)
	}
}

func TestClassifyStillPresentDisagreement(t *testing.T) {
	t.Parallel()

	// The listing missed the repository twice but upstream still serves it
	// under the same name: the listing, not the repository, is wrong.
	got, successor, update := Classify(
		probeTarget("git-repository-scope:acme/ghost", "acme/ghost", 204, cycleOne),
		ProbeResult{Found: true, FullName: "acme/ghost"},
	)
	if !update || got != UpstreamStillPresent || successor != "" {
		t.Fatalf("Classify() = (%q, %q, %v), want (still_present, \"\", true)", got, successor, update)
	}
}

func TestClassifyNotFound(t *testing.T) {
	t.Parallel()

	got, successor, update := Classify(
		probeTarget("git-repository-scope:acme/vanished", "acme/vanished", 205, cycleOne),
		ProbeResult{NotFound: true},
	)
	if !update || got != UpstreamNotFoundOrInaccessible || successor != "" {
		t.Fatalf("Classify() = (%q, %q, %v), want (not_found_or_inaccessible, \"\", true)", got, successor, update)
	}
}

func TestClassifyServerErrorKeepsPrior(t *testing.T) {
	t.Parallel()

	target := probeTarget("git-repository-scope:acme/flaky", "acme/flaky", 206, cycleOne)
	target.PriorClassification = UpstreamTransferred
	target.PriorSuccessor = "archive/flaky"
	got, successor, update := Classify(target, ProbeResult{})
	if update || got != UpstreamTransferred || successor != "archive/flaky" {
		t.Fatalf("Classify() = (%q, %q, %v), want prior (transferred, archive/flaky, false)", got, successor, update)
	}
}

func TestClassifyInconclusiveWithoutPriorWritesNothing(t *testing.T) {
	t.Parallel()

	got, successor, update := Classify(
		probeTarget("git-repository-scope:acme/fresh", "acme/fresh", 207, cycleOne),
		ProbeResult{},
	)
	if update || got != "" || successor != "" {
		t.Fatalf("Classify() = (%q, %q, %v), want (\"\", \"\", false)", got, successor, update)
	}
}

func confirmedNotListed(scope, slug string, since, evaluated time.Time, cycles int) Observation {
	return Observation{
		ScopeID: scope, State: StateNotListed, GitHubRepoID: 300,
		StateSince: since, StateCycleCount: cycles,
		EvaluatedAt: evaluated, LivenessWindow: testWindow,
	}
}

func TestSelectProbeTargetsConfirmedOnly(t *testing.T) {
	t.Parallel()

	since := cycleOne.Add(-time.Hour)
	evaluated := cycleOne
	prior := []Observation{
		confirmedNotListed("git-repository-scope:acme/confirmed", "acme/confirmed", since, evaluated, 2),
		// One evaluation only: pending, never probed.
		confirmedNotListed("git-repository-scope:acme/pending", "acme/pending", evaluated, evaluated, 1),
		{
			ScopeID: "git-repository-scope:acme/selected", State: StateSelected, GitHubRepoID: 301,
			StateSince: since, StateCycleCount: 2, EvaluatedAt: evaluated, LivenessWindow: testWindow,
		},
		{
			ScopeID: "git-repository-scope:acme/archived", State: StateArchivedExcluded, GitHubRepoID: 302,
			StateSince: since, StateCycleCount: 2, EvaluatedAt: evaluated, LivenessWindow: testWindow,
		},
		{
			ScopeID: "git-repository-scope:acme/rule", State: StateRuleExcluded, GitHubRepoID: 303,
			StateSince: since, StateCycleCount: 2, EvaluatedAt: evaluated, LivenessWindow: testWindow,
		},
		// Confirmed not_listed but unknown to the known-scope map: without a
		// slug neither the fallback nor the same-name comparison is possible.
		confirmedNotListed("git-repository-scope:acme/gone", "acme/gone", since, evaluated, 2),
	}
	known := map[string]KnownScope{
		"git-repository-scope:acme/confirmed": {ScopeID: "git-repository-scope:acme/confirmed", Slug: "acme/confirmed"},
		"git-repository-scope:acme/pending":   {ScopeID: "git-repository-scope:acme/pending", Slug: "acme/pending"},
		"git-repository-scope:acme/selected":  {ScopeID: "git-repository-scope:acme/selected", Slug: "acme/selected"},
		"git-repository-scope:acme/archived":  {ScopeID: "git-repository-scope:acme/archived", Slug: "acme/archived"},
		"git-repository-scope:acme/rule":      {ScopeID: "git-repository-scope:acme/rule", Slug: "acme/rule"},
	}
	targets := SelectProbeTargets(prior, known, "acme", evaluated)
	if len(targets) != 1 || targets[0].ScopeID != "git-repository-scope:acme/confirmed" {
		t.Fatalf("SelectProbeTargets() = %+v, want only the confirmed not_listed scope", targets)
	}
}

func TestSelectProbeTargetsCapOldestFirst(t *testing.T) {
	t.Parallel()

	var prior []Observation
	known := make(map[string]KnownScope)
	for i := range 30 {
		scope := "git-repository-scope:acme/scope-" + string(rune('a'+i/10)) + string(rune('0'+i%10))
		since := cycleOne.Add(-time.Duration(30-i) * time.Hour)
		prior = append(prior, confirmedNotListed(scope, "acme/x", since, cycleOne, 2))
		known[scope] = KnownScope{ScopeID: scope, Slug: "acme/x"}
	}
	targets := SelectProbeTargets(prior, known, "acme", cycleOne)
	if len(targets) != ProbeCapPerCycle {
		t.Fatalf("SelectProbeTargets() = %d targets, want cap %d", len(targets), ProbeCapPerCycle)
	}
	for i := 1; i < len(targets); i++ {
		if targets[i].StateSince.Before(targets[i-1].StateSince) {
			t.Fatalf("targets not oldest-first: %+v before %+v", targets[i-1], targets[i])
		}
	}
}

func TestSelectProbeTargetsReprobeGate(t *testing.T) {
	t.Parallel()

	since := cycleOne.Add(-2 * testWindow)
	old := confirmedNotListed("git-repository-scope:acme/old", "acme/old", since, cycleOne, 3)
	old.Classification = UpstreamTransferred
	old.ClassifiedAt = cycleOne.Add(-2 * testWindow)
	recent := confirmedNotListed("git-repository-scope:acme/recent", "acme/recent", since, cycleOne, 3)
	recent.Classification = UpstreamRenamed
	recent.ClassifiedAt = cycleOne.Add(-time.Hour)
	prior := []Observation{old, recent}
	known := map[string]KnownScope{
		"git-repository-scope:acme/old":    {ScopeID: "git-repository-scope:acme/old", Slug: "acme/old"},
		"git-repository-scope:acme/recent": {ScopeID: "git-repository-scope:acme/recent", Slug: "acme/recent"},
	}
	targets := SelectProbeTargets(prior, known, "acme", cycleOne)
	if len(targets) != 1 || targets[0].ScopeID != "git-repository-scope:acme/old" {
		t.Fatalf("SelectProbeTargets() = %+v, want only the scope classified longest ago", targets)
	}
	if targets[0].PriorClassification != UpstreamTransferred {
		t.Fatalf("target prior = %q, want transferred carried for keep-prior", targets[0].PriorClassification)
	}
}

func TestProjectCarriesVerdictOnlyForNotListed(t *testing.T) {
	t.Parallel()

	now := cycleOne.Add(time.Hour)
	prior := Observation{
		ScopeID: "git-repository-scope:acme/old", State: StateNotListed,
		StateSince: cycleOne, StateCycleCount: 2, EvaluatedAt: cycleOne,
		LivenessWindow: testWindow, Classification: UpstreamTransferred,
		SuccessorSlug: "archive/old", ClassifiedAt: cycleOne,
	}
	kept := project(prior, true, Row{ScopeID: prior.ScopeID, State: StateNotListed}, now, testWindow)
	if kept.Classification != UpstreamTransferred || kept.SuccessorSlug != "archive/old" || !kept.ClassifiedAt.Equal(cycleOne) {
		t.Fatalf("project(not_listed) = %+v, want the prior verdict carried", kept)
	}
	cleared := project(prior, true, Row{ScopeID: prior.ScopeID, State: StateSelected, GitHubRepoID: 9}, now, testWindow)
	if cleared.Classification != "" || cleared.SuccessorSlug != "" || !cleared.ClassifiedAt.IsZero() {
		t.Fatalf("project(selected) = %+v, want the verdict cleared like the upsert SQL", cleared)
	}
}

func TestUpstreamClassificationMatchesCheckConstraint(t *testing.T) {
	t.Parallel()

	for _, c := range []UpstreamClassification{UpstreamTransferred, UpstreamRenamed, UpstreamStillPresent, UpstreamNotFoundOrInaccessible} {
		if !validUpstreamClassification(c) {
			t.Fatalf("validUpstreamClassification(%q) = false, want true", c)
		}
	}
	if validUpstreamClassification("deleted") {
		t.Fatal("validUpstreamClassification(deleted) = true: 404 never reads deleted")
	}
	if validUpstreamClassification("") {
		t.Fatal("validUpstreamClassification(\"\") = true: empty means unclassified, never stored")
	}
}
