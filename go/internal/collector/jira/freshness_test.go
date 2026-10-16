// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func freshnessFixtureWindow() CollectionWindow {
	until := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	return CollectionWindow{
		Since: until.Add(-defaultUpdatedLookback),
		Until: until,
	}
}

func freshnessFixtureResult(observedAt time.Time) CollectionResult {
	return CollectionResult{
		Issues: []Issue{
			{
				ID:        "10001",
				Key:       "OPS-123",
				Summary:   "Investigate checkout alert",
				IssueType: Reference{ID: "10001", Name: "Task"},
				Status:    Reference{ID: "3", Name: "In Progress"},
				Project:   Reference{ID: "10000", Key: "OPS", Name: "Operations"},
				Assignee:  Reference{AccountID: "user-1", DisplayName: "Ada"},
				Reporter:  Reference{AccountID: "user-2", DisplayName: "Bo"},
				CreatedAt: observedAt.Add(-48 * time.Hour),
				UpdatedAt: observedAt.Add(-time.Hour),
			},
			{
				ID:        "10002",
				Key:       "OPS-124",
				Summary:   "Rotate credentials",
				IssueType: Reference{ID: "10002", Name: "Sub-task"},
				Status:    Reference{ID: "1", Name: "To Do"},
				Project:   Reference{ID: "10000", Key: "OPS", Name: "Operations"},
				CreatedAt: observedAt.Add(-24 * time.Hour),
				UpdatedAt: observedAt.Add(-2 * time.Hour),
			},
		},
		Transitions: map[string][]Transition{"10001": {{
			ID:        "20001",
			IssueID:   "10001",
			IssueKey:  "OPS-123",
			Field:     "status",
			From:      "To Do",
			To:        "In Progress",
			CreatedAt: observedAt.Add(-time.Hour),
		}}},
		ExternalLinks: map[string][]ExternalLink{"10001": {{
			ID:           "30001",
			IssueID:      "10001",
			IssueKey:     "OPS-123",
			Relationship: "relates to",
			Object:       LinkObject{URL: "https://github.com/example/app/pull/42", Title: "PR 42"},
		}}},
		IssueLimit: defaultIssueLimit,
		ObservedAt: observedAt,
	}
}

func freshnessFixtureEnvelopes(t *testing.T, result CollectionResult) []facts.Envelope {
	t.Helper()

	source := &ClaimedSource{collectorInstanceID: "jira-primary"}
	envs, err := source.envelopes(workflow.WorkItem{
		GenerationID:        "jira:generation-freshness",
		CurrentFencingToken: 11,
	}, TargetConfig{
		ScopeID: "jira:site:example",
		BaseURL: "https://example.atlassian.net",
	}, result, result.ObservedAt)
	if err != nil {
		t.Fatalf("envelopes() error = %v, want nil", err)
	}
	return envs
}

func freshnessHintForFixture(t *testing.T, window CollectionWindow, result CollectionResult) string {
	t.Helper()

	return freshnessHint(window, result, freshnessFixtureEnvelopes(t, result))
}

// TestFreshnessHintStableForByteIdenticalRetry proves idempotent retry: the
// same window over the same provider content mints the same hint, so a
// retried collection skips as a duplicate instead of storing again.
func TestFreshnessHintStableForByteIdenticalRetry(t *testing.T) {
	t.Parallel()

	window := freshnessFixtureWindow()
	observedAt := window.Until
	first := freshnessHintForFixture(t, window, freshnessFixtureResult(observedAt))
	second := freshnessHintForFixture(t, window, freshnessFixtureResult(observedAt))
	if first == "" {
		t.Fatal("freshnessHint() is blank, want a stable content digest")
	}
	if first != second {
		t.Fatalf("freshnessHint() changed for a byte-identical retry: %q vs %q", first, second)
	}
}

// TestFreshnessHintIgnoresEnvelopeOrder proves the digest is insensitive to
// emission order: the same facts in a different order share one hint.
func TestFreshnessHintIgnoresEnvelopeOrder(t *testing.T) {
	t.Parallel()

	window := freshnessFixtureWindow()
	result := freshnessFixtureResult(window.Until)
	forward := freshnessHint(window, result, freshnessFixtureEnvelopes(t, result))
	reversed := freshnessFixtureEnvelopes(t, result)
	slices.Reverse(reversed)
	backward := freshnessHint(window, result, reversed)
	if forward == "" {
		t.Fatal("freshnessHint() is blank, want a stable content digest")
	}
	if forward != backward {
		t.Fatalf("freshnessHint() changed when envelope order changed: %q vs %q", forward, backward)
	}
}

// TestFreshnessHintChangesAcrossWindows proves distinct collection windows
// store: the same issue content under a later window mints a new hint instead
// of skipping as unchanged.
func TestFreshnessHintChangesAcrossWindows(t *testing.T) {
	t.Parallel()

	firstWindow := freshnessFixtureWindow()
	secondWindow := CollectionWindow{
		Since: firstWindow.Since.Add(time.Hour),
		Until: firstWindow.Until.Add(time.Hour),
	}
	first := freshnessHintForFixture(t, firstWindow, freshnessFixtureResult(firstWindow.Until))
	second := freshnessHintForFixture(t, secondWindow, freshnessFixtureResult(secondWindow.Until))
	if first == second {
		t.Fatalf("freshnessHint() did not change across windows: %q", first)
	}
}

// TestFreshnessHintChangesOnIssueDrift proves a status-only change mints a
// new hint: same keys with drifted content must store, not skip.
func TestFreshnessHintChangesOnIssueDrift(t *testing.T) {
	t.Parallel()

	window := freshnessFixtureWindow()
	before := freshnessHintForFixture(t, window, freshnessFixtureResult(window.Until))
	drifted := freshnessFixtureResult(window.Until)
	drifted.Issues[0].Status = Reference{ID: "5", Name: "Resolved"}
	drifted.Issues[0].UpdatedAt = window.Until
	after := freshnessHintForFixture(t, window, drifted)
	if before == after {
		t.Fatalf("freshnessHint() did not change after issue drift: %q", before)
	}
}

// TestFreshnessHintChangesOnTransitionDrift proves a new changelog entry mints
// a new hint even when the issue fields stand still.
func TestFreshnessHintChangesOnTransitionDrift(t *testing.T) {
	t.Parallel()

	window := freshnessFixtureWindow()
	before := freshnessHintForFixture(t, window, freshnessFixtureResult(window.Until))
	drifted := freshnessFixtureResult(window.Until)
	drifted.Transitions["10001"] = append(drifted.Transitions["10001"], Transition{
		ID:        "20002",
		IssueID:   "10001",
		IssueKey:  "OPS-123",
		Field:     "priority",
		From:      "Medium",
		To:        "High",
		CreatedAt: window.Until,
	})
	after := freshnessHintForFixture(t, window, drifted)
	if before == after {
		t.Fatalf("freshnessHint() did not change after transition drift: %q", before)
	}
}

// TestFreshnessHintMarksSearchTruncation proves a capped search mints a
// different hint than a complete one over the same issues: the truncation
// marker is part of the identity, not just the content.
func TestFreshnessHintMarksSearchTruncation(t *testing.T) {
	t.Parallel()

	window := freshnessFixtureWindow()
	complete := freshnessHintForFixture(t, window, freshnessFixtureResult(window.Until))
	truncatedResult := freshnessFixtureResult(window.Until)
	truncatedResult.SearchTruncated = true
	truncated := freshnessHintForFixture(t, window, truncatedResult)
	if complete == truncated {
		t.Fatalf("freshnessHint() did not change when the search truncated: %q", complete)
	}
}

// TestFreshnessHintFoldsIssueLimit proves a cap change mints a new hint: the
// same issues under a wider limit are a different observation that must
// store, not skip.
func TestFreshnessHintFoldsIssueLimit(t *testing.T) {
	t.Parallel()

	window := freshnessFixtureWindow()
	narrow := freshnessHintForFixture(t, window, freshnessFixtureResult(window.Until))
	wideResult := freshnessFixtureResult(window.Until)
	wideResult.IssueLimit = maxIssueLimit
	wide := freshnessHintForFixture(t, window, wideResult)
	if narrow == wide {
		t.Fatalf("freshnessHint() did not change when the issue limit changed: %q", narrow)
	}
}

// TestFreshnessHintMarksPartialCollection proves completeness flags are part
// of the identity: partial failures and metadata warnings mint new hints.
func TestFreshnessHintMarksPartialCollection(t *testing.T) {
	t.Parallel()

	window := freshnessFixtureWindow()
	complete := freshnessHintForFixture(t, window, freshnessFixtureResult(window.Until))
	partialResult := freshnessFixtureResult(window.Until)
	partialResult.Stats.PartialFailures = 1
	partial := freshnessHintForFixture(t, window, partialResult)
	if complete == partial {
		t.Fatalf("freshnessHint() did not change on partial failures: %q", complete)
	}
	warnedResult := freshnessFixtureResult(window.Until)
	warnedResult.MetadataWarnings = []MetadataWarning{{
		MetadataType: "project",
		Reason:       "permission_hidden",
		FailureClass: FailurePermissionHidden,
		ProviderID:   "10000",
	}}
	warned := freshnessHintForFixture(t, window, warnedResult)
	if complete == warned {
		t.Fatalf("freshnessHint() did not change on metadata warnings: %q", complete)
	}
}

// TestFreshnessHintNonBlankForEmptyWindow proves an empty issue set still
// mints a usable hint: empty generations participate in duplicate detection,
// and distinct empty windows stay distinct so the backstop (not a blank hint)
// decides whether an empty successor may land.
func TestFreshnessHintNonBlankForEmptyWindow(t *testing.T) {
	t.Parallel()

	firstWindow := freshnessFixtureWindow()
	secondWindow := CollectionWindow{
		Since: firstWindow.Since.Add(time.Hour),
		Until: firstWindow.Until.Add(time.Hour),
	}
	empty := CollectionResult{IssueLimit: defaultIssueLimit, ObservedAt: firstWindow.Until}
	first := freshnessHint(firstWindow, empty, nil)
	if first == "" {
		t.Fatal("freshnessHint() is blank for an empty window, want a usable digest")
	}
	second := freshnessHint(secondWindow, empty, nil)
	if first == second {
		t.Fatalf("freshnessHint() did not change across empty windows: %q", first)
	}
}
