// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secgroup

import (
	"context"
	"sort"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudjoin"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// Local copies of the reducer-root test helpers this family's tests used before
// the move (issue #6061). Go test files cannot share unexported symbols across a
// package boundary, so each helper the moved tests still need is duplicated here
// verbatim rather than exported from the root for test-only use.

// anyToString forwards to [payloadcore.AnyToString] so the moved tests keep
// their pre-move call sites unchanged.
func anyToString(v any) string {
	return payloadcore.AnyToString(v)
}

// cloudResourceUID forwards to [cloudjoin.CloudResourceUID] so the moved tests
// keep their pre-move call sites unchanged.
func cloudResourceUID(accountID, region, resourceType, resourceID string) string {
	return cloudjoin.CloudResourceUID(accountID, region, resourceType, resourceID)
}

func awsResourceEnvelope(payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.AWSResourceFactKind,
		Payload:  payload,
	}
}

// resourceEnvelope is a small helper for join-index tests. account+region are
// part of the uid identity (the cross-account/region trust boundary).
func resourceEnvelope(accountID, region, resourceType, resourceID, arn string, anchors ...string) facts.Envelope {
	anchorVals := make([]any, 0, len(anchors))
	for _, a := range anchors {
		anchorVals = append(anchorVals, a)
	}
	return awsResourceEnvelope(map[string]any{
		"account_id":          accountID,
		"region":              region,
		"resource_type":       resourceType,
		"resource_id":         resourceID,
		"arn":                 arn,
		"correlation_anchors": anchorVals,
	})
}

// stubFactLoader replays a fixed envelope batch and counts loads.
type stubFactLoader struct {
	envelopes []facts.Envelope
	calls     int
}

func (f *stubFactLoader) ListFacts(_ context.Context, _, _ string) ([]facts.Envelope, error) {
	f.calls++
	return f.envelopes, nil
}

// recordingGraphProjectionPhasePublisher captures the readiness publications the
// handler emits so a test can assert the keyspace and phase it published.
type recordingGraphProjectionPhasePublisher struct {
	calls [][]gpphase.PhaseState
	err   error
}

func (r *recordingGraphProjectionPhasePublisher) PublishGraphProjectionPhases(
	_ context.Context,
	rows []gpphase.PhaseState,
) error {
	cloned := make([]gpphase.PhaseState, len(rows))
	copy(cloned, rows)
	r.calls = append(r.calls, cloned)
	return r.err
}

// fakeProjectedSourceLedger is a call-recording [reducercontract.ProjectedSourceLedger]
// double. It records call order and arguments but does not model persistence
// — the static list/prune/record return values are configured up front. Use
// this for handler tests that only assert wiring (which methods are called,
// in what order, with what arguments).
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

func metricHasAttrs(rm metricdata.ResourceMetrics, metricName string, attrs map[string]string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != metricName {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				matches := true
				for key, want := range attrs {
					got, ok := point.Attributes.Value(attribute.Key(key))
					if !ok || got.AsString() != want {
						matches = false
						break
					}
				}
				if matches && point.Value > 0 {
					return true
				}
			}
		}
	}
	return false
}
