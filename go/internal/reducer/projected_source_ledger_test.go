// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"time"
)

// fakeProjectedSourceLedger is a call-recording ProjectedSourceLedger double.
// It records call order and arguments but does not model persistence — the
// static list/prune/record return values are configured up front. Use this for
// handler tests that only assert wiring (which methods are called, in what
// order, with what arguments).
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

// statefulProjectedSourceLedger is an in-memory ProjectedSourceLedger that
// actually persists rows keyed by (evidenceSource, scopeID) -> set of source
// uids, mirroring postgres.ProjectedSourceEdgeStore closely enough to prove
// end-to-end, multi-generation ledger behavior: RecordProjectedSources upserts
// (never clears a prior generation's uids), ListSourceUIDsForScopes returns the
// full accumulated set until PruneForScopes clears it. This is the fixture the
// leak-safety regression tests drive across two sequential Handle() calls.
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
