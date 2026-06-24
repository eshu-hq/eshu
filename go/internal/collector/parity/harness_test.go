// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/parity"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func mustRun(t *testing.T, h *parity.Harness, sc parity.Scenario) parity.Result {
	t.Helper()
	result, err := h.Run(context.Background(), sc)
	if err != nil {
		t.Fatalf("harness run %q: %v", sc.Name, err)
	}
	return result
}

func TestHarnessHappyPathReachesReadback(t *testing.T) {
	t.Parallel()

	h := parity.New()
	sc := parity.NewScenario("jira-happy", scope.CollectorJira, "scope-1", "gen-1", 1).
		WithFacts(parity.AdmissibleFact("jira_issue", "jira:issue:1")).
		Expecting(parity.Expectation{
			ClaimOutcome:      parity.ClaimCompleted,
			ReadableFactKinds: []string{"jira_issue"},
		})

	result := mustRun(t, h, sc)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessDetectsWrongReadbackExpectation(t *testing.T) {
	t.Parallel()

	h := parity.New()
	// The fact is admissible and committed, so it WILL be readable; expecting an
	// empty readback must make the harness report a contract failure. This proves
	// the harness fails when reality and the declared readback contract diverge.
	sc := parity.NewScenario("jira-mismatch", scope.CollectorJira, "scope-1", "gen-1", 1).
		WithFacts(parity.AdmissibleFact("jira_issue", "jira:issue:1")).
		Expecting(parity.Expectation{
			ClaimOutcome:      parity.ClaimCompleted,
			ReadableFactKinds: []string{},
		})

	result := mustRun(t, h, sc)
	if result.ContractMet {
		t.Fatalf("expected contract failure when readback expectation is wrong, got pass: %#v", result)
	}
}

func TestHarnessFailsWhenExpectedFactIsWithheldFromReadback(t *testing.T) {
	t.Parallel()

	h := parity.New()
	// The fact is committed but classified permission_hidden, so it never reaches
	// readback. Declaring it as expected-readable must fail the contract: this is
	// the "fixture facts cannot reach the expected reducer/readback contract" case.
	sc := parity.NewScenario("withheld", scope.CollectorJira, "scope-1", "gen-1", 1).
		WithFacts(parity.PermissionHiddenFact("jira_issue", "jira:issue:1")).
		Expecting(parity.Expectation{
			ClaimOutcome:      parity.ClaimCompleted,
			ReadableFactKinds: []string{"jira_issue"},
		})

	result := mustRun(t, h, sc)
	if result.ContractMet {
		t.Fatalf("expected contract failure when an expected fact never reaches readback: %#v", result)
	}
	if len(result.ReadableFactKinds) != 0 {
		t.Fatalf("withheld fact must not be readable, got %v", result.ReadableFactKinds)
	}
}

func TestHarnessPermissionHiddenAndUnsupportedNeverReachReadback(t *testing.T) {
	t.Parallel()

	h := parity.New()
	sc := parity.NewScenario("mixed", scope.CollectorGrafana, "scope-1", "gen-1", 1).
		WithFacts(
			parity.AdmissibleFact("grafana_dashboard", "grafana:dash:1"),
			parity.PermissionHiddenFact("grafana_datasource", "grafana:ds:1"),
			parity.UnsupportedFact("grafana_unknown", "grafana:unknown:1"),
		).
		Expecting(parity.Expectation{
			ClaimOutcome:      parity.ClaimCompleted,
			ReadableFactKinds: []string{"grafana_dashboard"},
		})

	result := mustRun(t, h, sc)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	if h.ReadableCount() != 1 {
		t.Fatalf("readable count = %d, want 1 (hidden/unsupported must not be readable)", h.ReadableCount())
	}
}

func TestHarnessDuplicateDeliveryIsIdempotent(t *testing.T) {
	t.Parallel()

	h := parity.New()
	sc := parity.NewScenario("dup", scope.CollectorJira, "scope-1", "gen-1", 1).
		WithFacts(
			parity.AdmissibleFact("jira_issue", "jira:issue:1"),
			parity.AdmissibleFact("jira_issue", "jira:issue:2"),
		).
		Expecting(parity.Expectation{
			ClaimOutcome:      parity.ClaimCompleted,
			ReadableFactKinds: []string{"jira_issue"},
		})

	first := mustRun(t, h, sc)
	if err := first.Err(); err != nil {
		t.Fatal(err)
	}
	// Replay the identical claim/generation: same stable keys + fencing token.
	second := mustRun(t, h, sc)
	if err := second.Err(); err != nil {
		t.Fatal(err)
	}
	if h.ReadableCount() != 2 {
		t.Fatalf("readable count after duplicate delivery = %d, want 2 (idempotent)", h.ReadableCount())
	}
}

func TestHarnessStaleGenerationIsRejected(t *testing.T) {
	t.Parallel()

	h := parity.New()
	fresh := parity.NewScenario("fresh", scope.CollectorJira, "scope-1", "gen-2", 2).
		WithFacts(parity.AdmissibleFact("jira_issue", "jira:issue:1")).
		Expecting(parity.Expectation{ClaimOutcome: parity.ClaimCompleted, ReadableFactKinds: []string{"jira_issue"}})
	if err := mustRun(t, h, fresh).Err(); err != nil {
		t.Fatal(err)
	}

	// A later attempt with an OLDER fencing token must not overwrite readback.
	stale := parity.NewScenario("stale", scope.CollectorJira, "scope-1", "gen-1", 1).
		WithFacts(parity.AdmissibleFact("jira_issue_stale", "jira:issue:1"))
	result := mustRun(t, h, stale)
	if result.ClaimOutcome != parity.ClaimCompleted {
		t.Fatalf("stale claim outcome = %q, want completed (commit succeeds; readback supersedes)", result.ClaimOutcome)
	}
	if got := h.ReadableFactKinds(); len(got) != 1 || got[0] != "jira_issue" {
		t.Fatalf("readback after stale replay = %v, want [jira_issue] (stale rejected)", got)
	}
}

func TestHarnessRetryableCommitFailureDeadLetters(t *testing.T) {
	t.Parallel()

	h := parity.New()
	sc := parity.NewScenario("retry", scope.CollectorJira, "scope-1", "gen-1", 1).
		WithFacts(parity.AdmissibleFact("jira_issue", "jira:issue:1"))
	sc.CommitErr = errors.New("transient backend error")
	sc.Expect = parity.Expectation{
		ClaimOutcome:      parity.ClaimFailedRetryable,
		DeadLettered:      true,
		ReadableFactKinds: []string{},
	}

	result := mustRun(t, h, sc)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessTerminalCommitFailureDeadLetters(t *testing.T) {
	t.Parallel()

	h := parity.New()
	sc := parity.NewScenario("terminal", scope.CollectorJira, "scope-1", "gen-1", 1).
		WithFacts(parity.AdmissibleFact("jira_issue", "jira:issue:1"))
	sc.CommitErr = parity.TerminalError{Err: errors.New("stale fence"), Class: "stale_fence"}
	sc.Expect = parity.Expectation{
		ClaimOutcome:      parity.ClaimFailedTerminal,
		DeadLettered:      true,
		ReadableFactKinds: []string{},
	}

	result := mustRun(t, h, sc)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	// The dead-letter record carries the generic commit_failure class, while the
	// fine-grained terminal classification lands on the claim mutation.
	if result.DeadLetterClass != "commit_failure" {
		t.Fatalf("dead-letter class = %q, want commit_failure", result.DeadLetterClass)
	}
	if result.ClaimFailureClass != "stale_fence" {
		t.Fatalf("claim failure class = %q, want stale_fence", result.ClaimFailureClass)
	}
}

func TestHarnessAttemptBudgetExhaustionIsTerminal(t *testing.T) {
	t.Parallel()

	h := parity.New()
	sc := parity.NewScenario("budget", scope.CollectorJira, "scope-1", "gen-1", 1).
		WithFacts(parity.AdmissibleFact("jira_issue", "jira:issue:1"))
	sc.CommitErr = errors.New("transient backend error")
	sc.MaxAttempts = 3
	sc.WorkItem.AttemptCount = 3 // already at budget
	sc.Expect = parity.Expectation{
		ClaimOutcome:      parity.ClaimFailedTerminal,
		DeadLettered:      true,
		ReadableFactKinds: []string{}, // a terminal failure must leave readback clean
	}

	if err := mustRun(t, h, sc).Err(); err != nil {
		t.Fatal(err)
	}
}

func TestHarnessRetryThenReplaySucceedsAndClearsDeadLetter(t *testing.T) {
	t.Parallel()

	h := parity.New()
	failed := parity.NewScenario("attempt-1", scope.CollectorJira, "scope-1", "gen-1", 1).
		WithFacts(parity.AdmissibleFact("jira_issue", "jira:issue:1"))
	failed.CommitErr = errors.New("transient backend error")
	failed.Expect = parity.Expectation{ClaimOutcome: parity.ClaimFailedRetryable, DeadLettered: true, ReadableFactKinds: []string{}}
	if err := mustRun(t, h, failed).Err(); err != nil {
		t.Fatal(err)
	}

	replay := parity.NewScenario("attempt-2", scope.CollectorJira, "scope-1", "gen-1", 1).
		WithFacts(parity.AdmissibleFact("jira_issue", "jira:issue:1")).
		Expecting(parity.Expectation{ClaimOutcome: parity.ClaimCompleted, ReadableFactKinds: []string{"jira_issue"}})
	result := mustRun(t, h, replay)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	if !result.ReplayCompleted {
		t.Fatalf("expected dead-letter replay completion on successful replay")
	}
}

func TestHarnessSourceNotReadyReleasesClaim(t *testing.T) {
	t.Parallel()

	h := parity.New()
	sc := parity.NewScenario("not-ready", scope.CollectorJira, "scope-1", "gen-1", 1)
	sc.SourceNotReady = true
	sc.Expect = parity.Expectation{ClaimOutcome: parity.ClaimReleased, ReadableFactKinds: []string{}}

	if err := mustRun(t, h, sc).Err(); err != nil {
		t.Fatal(err)
	}
}

func TestSummarizeAggregatesReadinessPerCollector(t *testing.T) {
	t.Parallel()

	h := parity.New()
	good := mustRun(t, h, parity.NewScenario("jira-ok", scope.CollectorJira, "scope-1", "gen-1", 1).
		WithFacts(parity.AdmissibleFact("jira_issue", "jira:issue:1")).
		Expecting(parity.Expectation{ClaimOutcome: parity.ClaimCompleted, ReadableFactKinds: []string{"jira_issue"}}))

	h2 := parity.New()
	bad := mustRun(t, h2, parity.NewScenario("grafana-bad", scope.CollectorGrafana, "scope-2", "gen-1", 1).
		WithFacts(parity.AdmissibleFact("grafana_dashboard", "grafana:dash:1")).
		Expecting(parity.Expectation{ClaimOutcome: parity.ClaimCompleted, ReadableFactKinds: []string{}}))

	summaries := parity.Summarize(good, bad)
	if len(summaries) != 2 {
		t.Fatalf("summaries = %d, want 2", len(summaries))
	}
	// Sorted by collector kind: grafana before jira.
	if summaries[0].CollectorKind != "grafana" || summaries[0].ContractMet {
		t.Fatalf("grafana summary = %#v, want contract not met", summaries[0])
	}
	if summaries[1].CollectorKind != "jira" || !summaries[1].ContractMet || !summaries[1].ReadbackReached {
		t.Fatalf("jira summary = %#v, want contract met + readback reached", summaries[1])
	}
}

func TestHarnessReadbackKeysAreScopedBySourceIdentity(t *testing.T) {
	t.Parallel()

	// Two scopes emitting the same StableFactKey must both become readable; the
	// fact contract only guarantees stable keys are unique within a source scope.
	h := parity.New()
	scopeA := parity.NewScenario("scope-a", scope.CollectorJira, "scope-1", "gen-1", 1).
		WithFacts(parity.AdmissibleFact("jira_issue", "issue:1")).
		Expecting(parity.Expectation{ClaimOutcome: parity.ClaimCompleted})
	scopeB := parity.NewScenario("scope-b", scope.CollectorJira, "scope-2", "gen-1", 1).
		WithFacts(parity.AdmissibleFact("jira_issue", "issue:1")).
		Expecting(parity.Expectation{ClaimOutcome: parity.ClaimCompleted})

	if err := mustRun(t, h, scopeA).Err(); err != nil {
		t.Fatal(err)
	}
	if err := mustRun(t, h, scopeB).Err(); err != nil {
		t.Fatal(err)
	}
	if h.ReadableCount() != 2 {
		t.Fatalf("readable count = %d, want 2 (same stable key across two scopes must not collapse)", h.ReadableCount())
	}
}

func TestHarnessReadbackReachedIsPerScenario(t *testing.T) {
	t.Parallel()

	// One harness, two collectors: the first admits facts, the second's facts are
	// all withheld. The second must report ReadbackReached=false even though the
	// shared readback store is non-empty from the first.
	h := parity.New()
	admitted := mustRun(t, h, parity.NewScenario("jira", scope.CollectorJira, "scope-1", "gen-1", 1).
		WithFacts(parity.AdmissibleFact("jira_issue", "jira:1")).
		Expecting(parity.Expectation{ClaimOutcome: parity.ClaimCompleted, ReadableFactKinds: []string{"jira_issue"}}))
	if !admitted.ReadbackReached {
		t.Fatalf("first scenario should reach readback")
	}

	withheld := mustRun(t, h, parity.NewScenario("grafana", scope.CollectorGrafana, "scope-2", "gen-1", 1).
		WithFacts(parity.PermissionHiddenFact("grafana_dashboard", "grafana:1")).
		Expecting(parity.Expectation{ClaimOutcome: parity.ClaimCompleted}))
	if withheld.ReadbackReached {
		t.Fatalf("all-withheld scenario must not report ReadbackReached despite shared store: %#v", withheld)
	}

	summaries := parity.Summarize(admitted, withheld)
	for _, s := range summaries {
		if s.CollectorKind == "grafana" && s.ReadbackReached {
			t.Fatalf("grafana summary must not inherit readback from jira: %#v", s)
		}
		if s.CollectorKind == "jira" && !s.ReadbackReached {
			t.Fatalf("jira summary should report readback reached: %#v", s)
		}
	}
}

func TestHarnessUnchangedCompletesWithoutCommit(t *testing.T) {
	t.Parallel()

	h := parity.New()
	sc := parity.NewScenario("unchanged", scope.CollectorJira, "scope-1", "gen-1", 1)
	sc.Unchanged = true
	sc.Expect = parity.Expectation{ClaimOutcome: parity.ClaimCompleted, ReadableFactKinds: []string{}}

	if err := mustRun(t, h, sc).Err(); err != nil {
		t.Fatal(err)
	}
}
