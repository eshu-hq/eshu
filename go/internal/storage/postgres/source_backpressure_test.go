// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// sourceBackpressureFakeDB scripts QueryContext responses in call order and
// records ExecContext calls, so the consult proofs can assert latch upserts
// without a live database. It serves only the source-backpressure consults.
type sourceBackpressureFakeDB struct {
	responses []fakeRows
	queries   []string
	execs     []sourceBackpressureExec
}

// sourceBackpressureExec is one recorded ExecContext call.
type sourceBackpressureExec struct {
	query string
	args  []any
}

func (f *sourceBackpressureFakeDB) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	f.queries = append(f.queries, query)
	if len(f.responses) == 0 {
		return &fakeRows{}, nil
	}
	next := f.responses[0]
	f.responses = f.responses[1:]
	return &next, nil
}

func (f *sourceBackpressureFakeDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.execs = append(f.execs, sourceBackpressureExec{query: query, args: args})
	return sourceBackpressureResult{}, nil
}

// sourceBackpressureResult is a no-op sql.Result for latch upserts.
type sourceBackpressureResult struct{}

func (sourceBackpressureResult) LastInsertId() (int64, error) { return 0, nil }
func (sourceBackpressureResult) RowsAffected() (int64, error) { return 1, nil }

// sourceBackpressureTestConfig mirrors the Proposed #10882 thresholds at
// test scale: engage past 1h oldest claimable age with 25 claimable rows.
func sourceBackpressureTestConfig() workflow.SourceBackpressureConfig {
	return workflow.SourceBackpressureConfig{
		EngageAge:     time.Hour,
		ReleaseAge:    15 * time.Minute,
		DepthFloor:    25,
		GrantDelay:    2 * time.Second,
		MaxEngagement: 30 * time.Minute,
	}
}

// TestCheckSourceBackpressurePacesSlowedFamilyAndAdvancesLatch proves the
// family consult: the family's pending source is behind (old and deep), so
// the verdict paces on reducer_source, names the source, carries the grant
// delay, and writes the shared latch engagement.
func TestCheckSourceBackpressurePacesSlowedFamilyAndAdvancesLatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	fake := &sourceBackpressureFakeDB{responses: []fakeRows{
		{rows: [][]any{{"vuln-intel"}}},              // pending sources for family
		{rows: [][]any{{float64(7200), int64(400)}}}, // bounded probe: vuln-intel behind
		{rows: [][]any{}},                            // latches: none engaged
	}}
	store := NewWorkflowControlStore(fake)

	decision, err := store.CheckSourceBackpressure(context.Background(), workflow.ClaimSelector{
		CollectorKind:       scope.CollectorKind("vuln"),
		CollectorInstanceID: "vuln-primary",
	}, sourceBackpressureTestConfig(), now)
	if err != nil {
		t.Fatalf("CheckSourceBackpressure() error = %v, want nil", err)
	}
	if !decision.Paced {
		t.Fatal("Paced = false, want true for behind source")
	}
	if decision.Signal != workflow.ClaimPacingSignalReducerSource {
		t.Fatalf("Signal = %q, want reducer_source", decision.Signal)
	}
	if decision.Domain != "vuln-intel" {
		t.Fatalf("Domain = %q, want vuln-intel", decision.Domain)
	}
	if decision.RetryAfter != 2*time.Second {
		t.Fatalf("RetryAfter = %s, want 2s", decision.RetryAfter)
	}
	if len(fake.execs) != 1 {
		t.Fatalf("latch upserts = %d, want 1", len(fake.execs))
	}
	if got := fake.execs[0].args[0]; got != "reducer_source" {
		t.Fatalf("latch signal arg = %v, want reducer_source", got)
	}
	if got := fake.execs[0].args[1]; got != "vuln-intel" {
		t.Fatalf("latch domain arg = %v, want vuln-intel", got)
	}
	if got := fake.execs[0].args[2]; got != true {
		t.Fatalf("latch engaged arg = %v, want true", got)
	}
}

// TestCheckSourceBackpressureMixedFamilyStaysUnpaced proves the fairness
// bound: a family serving a slowed source and a healthy source is not
// whole-family throttled (#10882 decision 2). The slowed source's latch
// still engages so publication shows it.
func TestCheckSourceBackpressureMixedFamilyStaysUnpaced(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	fake := &sourceBackpressureFakeDB{responses: []fakeRows{
		{rows: [][]any{{"git"}, {"vuln-intel"}}},
		{rows: [][]any{{float64(60), int64(3)}}},     // bounded probe: git healthy
		{rows: [][]any{{float64(7200), int64(400)}}}, // bounded probe: vuln-intel behind
		{rows: [][]any{}},
	}}
	store := NewWorkflowControlStore(fake)

	decision, err := store.CheckSourceBackpressure(context.Background(), workflow.ClaimSelector{
		CollectorKind:       scope.CollectorKind("mixed"),
		CollectorInstanceID: "mixed-primary",
	}, sourceBackpressureTestConfig(), now)
	if err != nil {
		t.Fatalf("CheckSourceBackpressure() error = %v, want nil", err)
	}
	if decision.Paced {
		t.Fatal("Paced = true for mixed family with healthy source, want false")
	}
	if len(fake.execs) != 1 {
		t.Fatalf("latch upserts = %d, want 1 (vuln-intel engagement only)", len(fake.execs))
	}
}

// TestCheckSourceBackpressureForSourceReleasesAndResumes proves resume: an
// engaged latch whose oldest claimable age fell to the release age returns
// unslowed and writes the latch release, so intake resumes when the queue
// clears.
func TestCheckSourceBackpressureForSourceReleasesAndResumes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	fake := &sourceBackpressureFakeDB{responses: []fakeRows{
		{rows: [][]any{{float64(300), int64(10)}}},                        // bounded probe: 5m old
		{rows: [][]any{{"vuln-intel", true, now.Add(-10 * time.Minute)}}}, // latch engaged
	}}
	store := NewWorkflowControlStore(fake)

	decision, err := store.CheckSourceBackpressureForSource(
		context.Background(), "vuln-intel", sourceBackpressureTestConfig(), now)
	if err != nil {
		t.Fatalf("CheckSourceBackpressureForSource() error = %v, want nil", err)
	}
	if decision.Slowed {
		t.Fatal("Slowed = true after queue cleared, want false (resume)")
	}
	if len(fake.execs) != 1 {
		t.Fatalf("latch upserts = %d, want 1 (release)", len(fake.execs))
	}
	if got := fake.execs[0].args[2]; got != false {
		t.Fatalf("latch engaged arg = %v, want false (released)", got)
	}
}

// TestCheckSourceBackpressureDisabledReadsNothing proves the zero config is
// a true off switch: no query, no latch write, unpaced verdict.
func TestCheckSourceBackpressureDisabledReadsNothing(t *testing.T) {
	t.Parallel()

	fake := &sourceBackpressureFakeDB{}
	store := NewWorkflowControlStore(fake)

	decision, err := store.CheckSourceBackpressure(context.Background(), workflow.ClaimSelector{
		CollectorKind:       scope.CollectorKind("vuln"),
		CollectorInstanceID: "vuln-primary",
	}, workflow.SourceBackpressureConfig{}, time.Now())
	if err != nil {
		t.Fatalf("CheckSourceBackpressure() error = %v, want nil", err)
	}
	if decision.Paced {
		t.Fatal("Paced = true with disabled config, want false")
	}
	if len(fake.queries) != 0 || len(fake.execs) != 0 {
		t.Fatalf("disabled consult touched storage: %d queries, %d execs", len(fake.queries), len(fake.execs))
	}
}

// TestSourcePacingStatesPublishesSlowedStateAndReason proves AC6 at the
// store read: every signalled source appears with slowed state, reason, and
// the observation behind it, without mutating the latch.
func TestSourcePacingStatesPublishesSlowedStateAndReason(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	fake := &sourceBackpressureFakeDB{responses: []fakeRows{
		{rows: [][]any{{"git", float64(60), int64(3)}, {"vuln-intel", float64(7200), int64(400)}}},
		{rows: [][]any{{"vuln-intel", true, now.Add(-time.Hour)}}},
	}}
	store := NewWorkflowControlStore(fake)

	states, err := store.SourcePacingStates(context.Background(), sourceBackpressureTestConfig(), now)
	if err != nil {
		t.Fatalf("SourcePacingStates() error = %v, want nil", err)
	}
	if len(states) != 2 {
		t.Fatalf("states = %d, want 2", len(states))
	}
	bySource := map[string]workflow.SourcePacingState{}
	for _, state := range states {
		bySource[state.SourceSystem] = state
	}
	vuln := bySource["vuln-intel"]
	if !vuln.Slowed || vuln.Reason != workflow.SourceBackpressureReasonSourceBehind {
		t.Fatalf("vuln-intel state = %+v, want slowed with source_behind reason", vuln)
	}
	if vuln.ClaimableDepth != 400 || vuln.OldestClaimableAge != 2*time.Hour {
		t.Fatalf("vuln-intel observation = depth %d age %s, want 400 / 2h", vuln.ClaimableDepth, vuln.OldestClaimableAge)
	}
	if !vuln.Stuck {
		t.Fatal("vuln-intel Stuck = false, want true (engaged 1h past 30m bound, still behind)")
	}
	if git := bySource["git"]; git.Slowed {
		t.Fatalf("git state = %+v, want unslowed (other source unchanged)", git)
	}
	if len(fake.execs) != 0 {
		t.Fatalf("publication read wrote latch rows: %d execs, want 0", len(fake.execs))
	}
}

// TestBoundedSourceSignalQueryDerivesFromGrouped pins the S0 contract:
// the bounded probe is the shipped grouped signal with only the SELECT
// head, the source restriction, and the grouping rewritten. The CTE, the
// claimable predicate, and the exact attribution stay byte-identical so
// the two cannot drift; a shipped-query edit that moves a marker fails
// here instead of silently forking the consult.
func TestBoundedSourceSignalQueryDerivesFromGrouped(t *testing.T) {
	t.Parallel()

	grouped := sourceClaimableSignalQuery
	for _, marker := range []string{
		boundedSourceSignalSelectHead,
		boundedSourceSignalGroupTail,
		"AND (work.visible_at IS NULL OR work.visible_at <= $1)",
	} {
		if !strings.Contains(grouped, marker) {
			t.Fatalf("grouped signal lost marker %q: bounded probe would fork", marker)
		}
	}
	if strings.Contains(grouped, "= $2") {
		t.Fatal("grouped signal takes a source parameter, want whole-backlog scan")
	}
	bounded := boundedSourceSignalQuery()
	for _, marker := range []string{
		"AND (work.visible_at IS NULL OR work.visible_at <= $1)",
		"COALESCE(NULLIF(BTRIM(work.payload->>'source_system'), ''), 'unknown') IN ($2, 'unknown')",
		"COALESCE(NULLIF(BTRIM(work.payload->>'source_system'), ''), NULLIF(BTRIM(scope.source_system), ''), 'unknown') = $2",
	} {
		if !strings.Contains(bounded, marker) {
			t.Fatalf("bounded probe lost marker %q", marker)
		}
	}
	for _, banned := range []string{"GROUP BY", "ORDER BY", "AS source_system"} {
		if strings.Contains(bounded, banned) {
			t.Fatalf("bounded probe contains %q, want single-source aggregate", banned)
		}
	}
}
