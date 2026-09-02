// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package obscoverage

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// readyLookup builds a stub [gpphase.ReadinessLookup] that always returns the
// given (ready, found) pair, regardless of key or phase. Package-local copy of
// the root test package's helper of the same name (issue #6061) — Go test
// helpers are not exported, so each package that gates on readiness carries
// its own.
func readyLookup(ready, found bool) gpphase.ReadinessLookup {
	return func(_ gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
		return ready, found
	}
}

// fakeWorkloadIdentityExecer records every ExecContext call instead of
// touching a live database. Package-local copy of the root test package's
// helper of the same name (issue #6061).
type fakeWorkloadIdentityExecer struct {
	execs []fakeWorkloadIdentityExecCall
}

// fakeWorkloadIdentityExecCall is one recorded ExecContext invocation.
type fakeWorkloadIdentityExecCall struct {
	query string
	args  []any
}

// ExecContext implements [factwrite.Execer] by recording the call and
// returning a fixed successful result.
func (f *fakeWorkloadIdentityExecer) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.execs = append(f.execs, fakeWorkloadIdentityExecCall{query: query, args: args})
	return fakeWorkloadIdentityResult{}, nil
}

// fakeWorkloadIdentityResult is the fixed sql.Result fakeWorkloadIdentityExecer
// returns.
type fakeWorkloadIdentityResult struct{}

func (fakeWorkloadIdentityResult) LastInsertId() (int64, error) { return 0, nil }
func (fakeWorkloadIdentityResult) RowsAffected() (int64, error) { return 1, nil }

// stubFactLoader is a minimal [factload.FactLoader] double that returns a
// fixed envelope set. Package-local copy of the root test package's helper of
// the same name (issue #6061).
type stubFactLoader struct {
	envelopes []facts.Envelope
	calls     int
}

func (f *stubFactLoader) ListFacts(_ context.Context, _, _ string) ([]facts.Envelope, error) {
	f.calls++
	return f.envelopes, nil
}

// fakeProjectedSourceLedger is a call-recording [reducercontract.ProjectedSourceLedger]
// double. It records call order and arguments but does not model persistence
// — the static list/prune/record return values are configured up front. Use
// this for handler tests that only assert wiring (which methods are called,
// in what order, with what arguments). Package-local copy of the root test
// package's helper of the same name (issue #6061).
type fakeProjectedSourceLedger struct {
	listUIDs []string
	listErr  error

	recordCalls    int
	recordedUIDs   []string
	recordedSource string
	recordedScope  string
	recordedGen    string

	pruneCalls  int
	prunedScope []string

	callOrder []string
}

func (f *fakeProjectedSourceLedger) RecordProjectedSources(
	_ context.Context,
	evidenceSource string,
	scopeID string,
	generationID string,
	sourceUIDs []string,
	_ time.Time,
) error {
	f.recordCalls++
	f.recordedUIDs = append(f.recordedUIDs, sourceUIDs...)
	f.recordedSource = evidenceSource
	f.recordedScope = scopeID
	f.recordedGen = generationID
	f.callOrder = append(f.callOrder, "record")
	return nil
}

func (f *fakeProjectedSourceLedger) ListSourceUIDsForScopes(
	_ context.Context, _ string, scopeIDs []string,
) ([]string, error) {
	f.callOrder = append(f.callOrder, "list")
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.prunedScope = scopeIDs
	return f.listUIDs, nil
}

func (f *fakeProjectedSourceLedger) PruneForScopes(
	_ context.Context, _ string, _ []string,
) error {
	f.pruneCalls++
	f.callOrder = append(f.callOrder, "prune")
	return nil
}

// statefulProjectedSourceLedger is an in-memory
// [reducercontract.ProjectedSourceLedger] that actually persists rows keyed by
// (evidenceSource, scopeID) -> set of source uids, mirroring
// postgres.ProjectedSourceEdgeStore closely enough to prove end-to-end,
// multi-generation ledger behavior: RecordProjectedSources upserts (never
// clears a prior generation's uids), ListSourceUIDsForScopes returns the full
// accumulated set until PruneForScopes clears it. This is the fixture the
// leak-safety regression tests drive across two sequential Handle() calls.
// Package-local copy of the root test package's helper of the same name
// (issue #6061).
type statefulProjectedSourceLedger struct {
	rows map[string]map[string]struct{} // key(evidenceSource, scopeID) -> uid set
}

func newStatefulProjectedSourceLedger() *statefulProjectedSourceLedger {
	return &statefulProjectedSourceLedger{rows: make(map[string]map[string]struct{})}
}

func (l *statefulProjectedSourceLedger) key(evidenceSource, scopeID string) string {
	return evidenceSource + "|" + scopeID
}

func (l *statefulProjectedSourceLedger) RecordProjectedSources(
	_ context.Context,
	evidenceSource string,
	scopeID string,
	_ string,
	sourceUIDs []string,
	_ time.Time,
) error {
	key := l.key(evidenceSource, scopeID)
	set, ok := l.rows[key]
	if !ok {
		set = make(map[string]struct{})
		l.rows[key] = set
	}
	for _, uid := range sourceUIDs {
		set[uid] = struct{}{}
	}
	return nil
}

func (l *statefulProjectedSourceLedger) ListSourceUIDsForScopes(
	_ context.Context, evidenceSource string, scopeIDs []string,
) ([]string, error) {
	seen := make(map[string]struct{})
	for _, scopeID := range scopeIDs {
		for uid := range l.rows[l.key(evidenceSource, scopeID)] {
			seen[uid] = struct{}{}
		}
	}
	uids := make([]string, 0, len(seen))
	for uid := range seen {
		uids = append(uids, uid)
	}
	sort.Strings(uids)
	return uids, nil
}

func (l *statefulProjectedSourceLedger) PruneForScopes(
	_ context.Context, evidenceSource string, scopeIDs []string,
) error {
	for _, scopeID := range scopeIDs {
		delete(l.rows, l.key(evidenceSource, scopeID))
	}
	return nil
}
