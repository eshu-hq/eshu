// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

// Skip-and-park proofs for the phase-two ArgoCD config reload (arbiter
// ruling, combination A+C): a 57014 statement_timeout on a config page maps
// onto ErrRelationshipFactLoadBudgetExceeded and must degrade the pass —
// park the config repos, withhold the whole fan-in — instead of failing the
// pod. A non-sentinel phase-two error keeps today's fail-loud path.

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// appsetGeneratorFactRow is a phase-one content fact carrying an ApplicationSet
// whose git generator reads the "config" repo, so
// ResolveArgoCDGeneratorConfigRepos resolves configRepoIDs ["repo-config"]
// against the degrade fixture catalog below.
func appsetGeneratorFactRow() []any {
	return contentFactRow("fact-appset", "scope-app", "gen-app", "content",
		`{"repo_id":"repo-app","artifact_type":"argocd","relative_path":"appset.yaml",`+
			`"content":"kind: ApplicationSet\nspec:\n  generators:\n    - git:\n`+
			`        repoURL: config\n        files:\n          - path: apps/*/config.yaml\n`+
			`  template:\n    spec:\n      source:\n        repoURL: '{{.path.basenameNormalized}}'\n"}`)
}

// newPhaseTwoDegradeStore builds a Store over a three-repo catalog where the
// ApplicationSet in scope-app resolves a phase-two config reload of
// repo-config. configErr is injected on the phase-two page queries; nil loads
// empty config pages. activeGenApp overrides scope-app's under-lock
// generation for the combined-guard case ("" keeps gen-app live).
func newPhaseTwoDegradeStore(
	t *testing.T,
	configErr error,
	activeGenApp string,
) (Store, *blockPassDB, *recordingEvidenceStore, *recordingPhaseStore) {
	t.Helper()

	observedNew := time.Date(2026, time.June, 24, 12, 0, 0, 0, time.UTC)
	genApp := "gen-app"
	if activeGenApp != "" {
		genApp = activeGenApp
	}
	database := &blockPassDB{
		catalogRows: [][]any{
			{[]byte(`{"repo_id":"repo-infra","name":"infra-repo"}`), observedNew},
			{[]byte(`{"repo_id":"repo-app","name":"app-repo"}`), observedNew},
			{[]byte(`{"repo_id":"repo-config","name":"config"}`), observedNew},
		},
		partitions: [][]any{
			{"scope-app", "gen-app"},
			{"scope-infra", "gen-infra"},
		},
		factsByScope: map[string][][]any{
			"scope-app": {
				appsetGeneratorFactRow(),
			},
			"scope-infra": {
				contentFactRow(
					"fact-cross", "scope-infra", "gen-infra", "content",
					`{"repo_id":"repo-infra","artifact_type":"terraform","relative_path":"main.tf","content":"app_repo = \"app-repo\""}`,
				),
			},
		},
		activeGens: [][]any{
			{"repo-app", "scope-app", genApp},
			{"repo-infra", "scope-infra", "gen-infra"},
		},
		configErr: configErr,
	}
	store, evidence, phases := testStore(database)
	store.Now = func() time.Time { return time.Unix(0, 0).UTC() }
	store.LoadCatalog = LoadRepositoryCatalog
	store.BatchSize = 1
	store.Workers = 2
	return store, database, evidence, phases
}

// countBlockLines counts log lines for one block under one block-line marker.
func countBlockLines(output, marker, block string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		rest := line[idx+len(marker):]
		fields := strings.Fields(rest)
		if len(fields) > 0 && fields[0] == "block="+block {
			count++
		}
	}
	return count
}

// capturePassLog runs fn with the std log captured and returns the output.
func capturePassLog(t *testing.T, fn func()) string {
	t.Helper()
	original := log.Writer()
	t.Cleanup(func() { log.SetOutput(original) })
	var buf bytes.Buffer
	log.SetOutput(&buf)
	fn()
	return buf.String()
}

// TestPagedConfigLoadStatementTimeoutMapsToBudgetSentinel is the rung-1a
// characterization pin: a 57014 on a config page maps onto
// ErrRelationshipFactLoadBudgetExceeded with the 5m0s message. It passes on
// current code and closes the unpinned-mapping gap.
func TestPagedConfigLoadStatementTimeoutMapsToBudgetSentinel(t *testing.T) {
	t.Parallel()

	payload := `{"repo_id":"repo-config","content_path":"app.yaml","content":"kind: Application"}`
	provider := &fakePageConnProvider{
		pages: []queueFakeRows{
			{rows: [][]any{contentFactRow("cfg-1", "scope-c", "gen-c", "content", payload)}},
			{rows: [][]any{contentFactRow("cfg-2", "scope-c", "gen-c", "content", payload)}},
		},
		queryErrs: map[int]error{1: &pgconn.PgError{
			Code:    "57014",
			Message: "canceling statement due to statement timeout",
		}},
		registry: newFakeSnapshotRegistry(),
	}
	ctx := WithBackfillApplicationName(context.Background(), BackfillApplicationNameDeferred)

	_, err := loadArgoCDGeneratorConfigFactsPaged(ctx, provider, []string{"repo-config"}, 1)
	if !errors.Is(err, ErrRelationshipFactLoadBudgetExceeded) {
		t.Fatalf("err = %v, want ErrRelationshipFactLoadBudgetExceeded", err)
	}
	if !strings.Contains(err.Error(), "argocd generator config fact load timed out after 5m0s") {
		t.Fatalf("err = %q, want the 5m0s timeout message", err)
	}
}

// TestRunAllRelationshipEvidenceDegradesOnPhaseTwoTimeout is the rung-1b RED
// test: a phase-two 57014 must NOT fail the pass. Phase-one evidence still
// commits, exactly one fact_load block_degraded line (zero block_failed)
// carries the parked counts, the clean completed line is absent, and no
// readiness, memo, or reopen-redundant claim is published for parked work.
func TestRunAllRelationshipEvidenceDegradesOnPhaseTwoTimeout(t *testing.T) {
	timeout := &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}
	store, database, evidence, phases := newPhaseTwoDegradeStore(t, timeout, "")

	var unchanged map[ScopeGenerationPartition]struct{}
	var runErr error
	output := capturePassLog(t, func() {
		unchanged, runErr = store.RunAllRelationshipEvidence(context.Background(), nil, nil)
	})
	if runErr != nil {
		t.Fatalf("RunAllRelationshipEvidence() error = %v, want nil (degraded pass continues)", runErr)
	}
	if len(evidence.calls) == 0 {
		t.Fatal("evidence calls = 0, want phase-one evidence committed on the degraded pass")
	}
	if len(phases.rows) != 0 {
		t.Fatalf("phase rows = %d, want 0 (whole fan-in withheld on degraded pass)", len(phases.rows))
	}
	if len(unchanged) != 0 {
		t.Fatalf("unchanged partitions = %d, want 0 (parked work is never reopen-redundant)", len(unchanged))
	}
	if got := countBlockLines(output, blockDegradedLogName+" ", BlockFactLoad); got != 1 {
		t.Fatalf("fact_load block_degraded lines = %d, want exactly 1\noutput:\n%s", got, output)
	}
	if got := countBlockLines(output, blockFailedLogName+" ", BlockFactLoad); got != 0 {
		t.Fatalf("fact_load block_failed lines = %d, want 0\noutput:\n%s", got, output)
	}
	if strings.Contains(output, "deferred_backfill_completed ") {
		t.Fatalf("clean deferred_backfill_completed line present on a degraded pass\noutput:\n%s", output)
	}
	if !strings.Contains(output, "deferred_backfill_completed_degraded ") {
		t.Fatalf("degraded completion line missing\noutput:\n%s", output)
	}
	for _, stmt := range database.recordedStatements() {
		if strings.Contains(stmt, "deferred_backfill_partition_memo") &&
			strings.Contains(stmt, "INSERT INTO") {
			t.Fatalf("memo write issued on a degraded pass: %q", stmt)
		}
	}
}

// TestDegradedPassSkipSetStaysAuthoritativeEmpty pins the reopen-storm
// bound's degraded-pass leg: a degraded pass returns an empty but NON-NIL
// skip-set. The Track-1 gate already ran, so "no partition is proven
// redundant once phase two is parked" is authoritative same-pass data — the
// reopen treats every candidate as a miss WITH the maintenance reopen
// circuit breaker applied. A nil skip-set would exempt the pass from the
// breaker and reopen the floor unconditionally, which is exactly the storm
// shape a run of degraded passes produces while memo publication stays
// withheld.
func TestDegradedPassSkipSetStaysAuthoritativeEmpty(t *testing.T) {
	timeout := &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}
	store, _, _, _ := newPhaseTwoDegradeStore(t, timeout, "")

	var unchanged map[ScopeGenerationPartition]struct{}
	capturePassLog(t, func() {
		var runErr error
		unchanged, runErr = store.RunAllRelationshipEvidence(context.Background(), nil, nil)
		if runErr != nil {
			t.Fatalf("RunAllRelationshipEvidence() error = %v, want nil (degraded pass continues)", runErr)
		}
	})
	if unchanged == nil {
		t.Fatal("degraded pass skip-set = nil, want authoritative empty (nil would exempt the reopen circuit breaker)")
	}
	if len(unchanged) != 0 {
		t.Fatalf("degraded pass skip-set has %d members, want 0 (nothing is proven redundant once phase two is parked)", len(unchanged))
	}
}

// TestPhaseTwoNonSentinelErrorStillFailsPass is seeded-violation A
// (classification fence): a generic phase-two error must keep today's
// fail-loud path — pass error plus block_failed, never a degrade.
func TestPhaseTwoNonSentinelErrorStillFailsPass(t *testing.T) {
	store, _, _, phases := newPhaseTwoDegradeStore(t, errors.New("connection refused"), "")

	var runErr error
	output := capturePassLog(t, func() {
		_, runErr = store.RunAllRelationshipEvidence(context.Background(), nil, nil)
	})
	if runErr == nil {
		t.Fatal("RunAllRelationshipEvidence() error = nil, want the loud phase-two failure")
	}
	if errors.Is(runErr, ErrRelationshipFactLoadBudgetExceeded) {
		t.Fatalf("error = %v, must not map a generic failure to the budget sentinel", runErr)
	}
	if got := countBlockLines(output, blockFailedLogName+" ", BlockFactLoad); got != 1 {
		t.Fatalf("fact_load block_failed lines = %d, want exactly 1\noutput:\n%s", got, output)
	}
	if got := countBlockLines(output, blockDegradedLogName+" ", BlockFactLoad); got != 0 {
		t.Fatalf("fact_load block_degraded lines = %d, want 0 for a non-sentinel error\noutput:\n%s", got, output)
	}
	if len(phases.rows) != 0 {
		t.Fatalf("phase rows = %d, want 0 on a failed pass", len(phases.rows))
	}
}

// TestDegradedPassWithholdsAdvancedGenerationOnce is the combined-guard case:
// scope-app's generation advanced since the snapshot AND phase two degraded.
// The advanced partition is skipped once by the snapshot guard (no
// double-publish accounting), nothing publishes, and the pass still returns
// nil with no reopen-redundant claim.
func TestDegradedPassWithholdsAdvancedGenerationOnce(t *testing.T) {
	timeout := &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}
	store, _, evidence, phases := newPhaseTwoDegradeStore(t, timeout, "gen-app-2")

	var unchanged map[ScopeGenerationPartition]struct{}
	var runErr error
	output := capturePassLog(t, func() {
		unchanged, runErr = store.RunAllRelationshipEvidence(context.Background(), nil, nil)
	})
	if runErr != nil {
		t.Fatalf("RunAllRelationshipEvidence() error = %v, want nil", runErr)
	}
	if len(phases.rows) != 0 {
		t.Fatalf("phase rows = %d, want 0", len(phases.rows))
	}
	if len(unchanged) != 0 {
		t.Fatalf("unchanged partitions = %d, want 0", len(unchanged))
	}
	if got := countBlockLines(output, blockFailedLogName+" ", BlockFactLoad); got != 0 {
		t.Fatalf("fact_load block_failed lines = %d, want 0\noutput:\n%s", got, output)
	}
	_ = evidence
}
