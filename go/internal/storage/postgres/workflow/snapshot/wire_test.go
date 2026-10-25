// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package snapshot_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
	"github.com/eshu-hq/eshu/go/internal/status/shared"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/workflow/snapshot"
)

var base = time.Date(2026, 10, 25, 9, 0, 0, 123456789, time.UTC)

// fullSnapshot fills every field the live readCoordinatorSnapshot can set,
// including the terminal failures that the status JSON excludes.
func fullSnapshot() *statuspkg.CoordinatorSnapshot {
	return &statuspkg.CoordinatorSnapshot{
		CollectorInstances: []collector.InstanceSummary{{
			InstanceID: "inst-1", CollectorKind: "git", Mode: "continuous", Enabled: true, Bootstrap: true,
			ClaimsEnabled: true, DisplayName: "Git", LastObservedAt: base, UpdatedAt: base.Add(time.Second),
			DeactivatedAt: base.Add(time.Hour), ScanInterval: 15 * time.Minute, ExtensionHosted: true,
		}, {
			InstanceID: "inst-2", CollectorKind: "aws", Mode: "manual", LastObservedAt: base, UpdatedAt: base,
		}},
		RunStatusCounts:      []shared.NamedCount{{Name: "completed", Count: 7}, {Name: "failed", Count: 1}},
		WorkItemStatusCounts: []shared.NamedCount{{Name: "completed", Count: 498000}, {Name: "failed_terminal", Count: 60000}},
		CompletenessCounts:   []shared.NamedCount{{Name: "complete", Count: 3}},
		CollectorBackpressure: []collector.BackpressureSnapshot{{
			CollectorKind: "git", CollectorInstanceID: "inst-1", SourceSystem: "git",
			Pending: 4, Claimed: 3, Retrying: 2, DeadLetter: 1, TerminalFailed: 60, Expired: 5,
			ActiveClaims: 3, OverdueClaims: 1,
			OldestPendingAge: 90 * time.Second, OldestRetryAge: 30 * time.Second,
			OldestClaimAge: 10 * time.Second, NextRetryDelay: 20 * time.Second,
			FailureClassCounts: []shared.NamedCount{{Name: "timeout", Count: 62}},
		}, {
			CollectorKind: "aws", CollectorInstanceID: "inst-2", SourceSystem: "aws",
		}},
		CollectorTerminalFailures: []collector.TerminalFailure{{
			CollectorKind: "git", InstanceID: "inst-1", ScopeID: "scope-1", WorkItemID: "wi-1",
			FailureClass: "timeout", FailureOperation: "clone", ObservedAt: base,
		}, {
			CollectorKind: "git", InstanceID: "inst-1", ScopeID: "scope-2", WorkItemID: "wi-2",
			FailureClass: "auth", FailureOperation: "fetch", ObservedAt: base, Acknowledged: true,
			AcknowledgedAt: base.Add(time.Minute),
		}},
		ActiveClaims:     3,
		OverdueClaims:    1,
		OldestPendingAge: 95 * time.Second,
		RecentFailures: &statuspkg.CoordinatorRecentFailures{
			Window: 30 * time.Minute, FailedRuns: 1, BlockedCompleteness: 2, TerminalWorkItems: 3,
		},
	}
}

func TestEncodeDecodeRoundTripsEveryField(t *testing.T) {
	t.Parallel()
	want := fullSnapshot()
	entries, err := snapshot.Encode(want)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	got, err := snapshot.Decode(entries)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip differs\n got: %#v\nwant: %#v", got, want)
	}
}

func TestEncodeDecodeNilSnapshotRoundTripsAsNil(t *testing.T) {
	t.Parallel()
	entries, err := snapshot.Encode(nil)
	if err != nil {
		t.Fatalf("Encode(nil) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("Encode(nil) entries = %d, want 0", len(entries))
	}
	got, err := snapshot.Decode(entries)
	if err != nil || got != nil {
		t.Fatalf("Decode(empty) = %#v, %v; want nil, nil", got, err)
	}
}

func TestDecodeEmptySectionsAreNonNilLikeTheLiveRead(t *testing.T) {
	t.Parallel()
	entries, err := snapshot.Encode(&statuspkg.CoordinatorSnapshot{
		CollectorInstances: []collector.InstanceSummary{}, RunStatusCounts: []shared.NamedCount{},
		WorkItemStatusCounts: []shared.NamedCount{}, CompletenessCounts: []shared.NamedCount{},
		CollectorBackpressure: []collector.BackpressureSnapshot{},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := snapshot.Decode(entries)
	if err != nil {
		t.Fatal(err)
	}
	if got.CollectorInstances == nil || got.RunStatusCounts == nil || got.WorkItemStatusCounts == nil ||
		got.CompletenessCounts == nil || got.CollectorBackpressure == nil {
		t.Fatalf("empty live sections must decode to empty non-nil slices: %#v", got)
	}
	if got.CollectorTerminalFailures != nil || got.RecentFailures != nil {
		t.Fatalf("absent terminal failures and recent failures must stay nil: %#v", got)
	}
}

func TestDecodeRejectsWhatItCannotFullyUnderstand(t *testing.T) {
	t.Parallel()
	good, err := snapshot.Encode(fullSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	mutate := func(f func([]summary.Entry) []summary.Entry) []summary.Entry {
		return f(append([]summary.Entry(nil), good...))
	}
	cases := map[string][]summary.Entry{
		"unknown section": mutate(func(e []summary.Entry) []summary.Entry {
			return append(e, summary.Entry{Section: "mystery", Ordinal: 1, JSON: `{}`})
		}),
		"unknown field": mutate(func(e []summary.Entry) []summary.Entry {
			e[0].JSON = strings.Replace(e[0].JSON, "{", `{"surprise":1,`, 1)
			return e
		}),
		"trailing data": mutate(func(e []summary.Entry) []summary.Entry {
			e[0].JSON += ` {}`
			return e
		}),
		"missing claims row": mutate(func(e []summary.Entry) []summary.Entry {
			var kept []summary.Entry
			for _, entry := range e {
				if entry.Section != "claims" {
					kept = append(kept, entry)
				}
			}
			return kept
		}),
		"duplicate claims row": mutate(func(e []summary.Entry) []summary.Entry {
			for _, entry := range e {
				if entry.Section == "claims" {
					return append(e, entry)
				}
			}
			return e
		}),
		"bad timestamp": mutate(func(e []summary.Entry) []summary.Entry {
			for i := range e {
				if e[i].Section == "instance" {
					e[i].JSON = strings.Replace(e[i].JSON, base.Format(time.RFC3339Nano), "not-a-time", 1)
					break
				}
			}
			return e
		}),
	}
	for name, entries := range cases {
		if _, err := snapshot.Decode(entries); err == nil {
			t.Errorf("%s: Decode() error = nil, want an error", name)
		}
	}
}

func TestAdvanceAgesMovesEveryAgeAndShrinksTheRetryDelay(t *testing.T) {
	t.Parallel()
	snap := fullSnapshot()
	snapshot.AdvanceAges(snap, 7*time.Second)
	row := snap.CollectorBackpressure[0]
	if snap.OldestPendingAge != 102*time.Second {
		t.Errorf("OldestPendingAge = %s, want 1m42s", snap.OldestPendingAge)
	}
	if row.OldestPendingAge != 97*time.Second || row.OldestRetryAge != 37*time.Second || row.OldestClaimAge != 17*time.Second {
		t.Errorf("backpressure ages = %s/%s/%s", row.OldestPendingAge, row.OldestRetryAge, row.OldestClaimAge)
	}
	if row.NextRetryDelay != 13*time.Second {
		t.Errorf("NextRetryDelay = %s, want 13s", row.NextRetryDelay)
	}
	empty := snap.CollectorBackpressure[1]
	if empty.OldestPendingAge != 0 || empty.OldestRetryAge != 0 || empty.OldestClaimAge != 0 || empty.NextRetryDelay != 0 {
		t.Errorf("a zero age means an empty set and must stay zero: %#v", empty)
	}
	snapshot.AdvanceAges(snap, time.Hour)
	if snap.CollectorBackpressure[0].NextRetryDelay != 0 {
		t.Errorf("NextRetryDelay must clamp at zero, got %s", snap.CollectorBackpressure[0].NextRetryDelay)
	}
}

func TestAdvanceAgesIgnoresNilAndNonPositiveAges(t *testing.T) {
	t.Parallel()
	snapshot.AdvanceAges(nil, time.Minute)
	snap := fullSnapshot()
	snapshot.AdvanceAges(snap, 0)
	snapshot.AdvanceAges(snap, -time.Minute)
	if !reflect.DeepEqual(snap, fullSnapshot()) {
		t.Fatal("a non-positive age must change nothing")
	}
}

func TestSourceSHA256DependsOnEveryStatementAndTheEncoding(t *testing.T) {
	t.Parallel()
	a := snapshot.SourceSHA256("one", "two")
	if a == snapshot.SourceSHA256("one", "twp") || a == snapshot.SourceSHA256("one") || a == snapshot.SourceSHA256("two", "one") {
		t.Fatal("digest must change with any statement text or order")
	}
	if a != snapshot.SourceSHA256("one", "two") {
		t.Fatal("digest must be deterministic")
	}
	if len(a) != 64 {
		t.Fatalf("digest length = %d, want 64 hex", len(a))
	}
}

func TestCloneSharesNoMemoryWithTheOriginal(t *testing.T) {
	t.Parallel()
	orig := fullSnapshot()
	cloned := snapshot.Clone(orig)
	if !reflect.DeepEqual(cloned, orig) {
		t.Fatal("clone must equal the original")
	}
	snapshot.AdvanceAges(cloned, time.Minute)
	cloned.CollectorBackpressure[0].FailureClassCounts[0].Count = 999
	cloned.RecentFailures.FailedRuns = 999
	cloned.CollectorTerminalFailures[0].ScopeID = "changed"
	cloned.CollectorInstances[0].InstanceID = "changed"
	if !reflect.DeepEqual(orig, fullSnapshot()) {
		t.Fatal("mutating the clone changed the original")
	}
	if snapshot.Clone(nil) != nil {
		t.Fatal("Clone(nil) must be nil")
	}
}

// TestFixtureSetsEveryExportedField is the guard behind the round-trip test: a
// hand-built fixture cannot see a field added later, so this walks the fixture
// by reflection and fails when any exported field of any struct reachable from
// the snapshot is zero in every occurrence. A new field on the status types
// then fails here until the fixture, the wire shape, and Encode and Decode
// cover it.
func TestFixtureSetsEveryExportedField(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer:
			if !v.IsNil() {
				walk(v.Elem())
			}
		case reflect.Slice:
			for i := range v.Len() {
				walk(v.Index(i))
			}
		case reflect.Struct:
			if v.Type() == reflect.TypeOf(time.Time{}) {
				return
			}
			for i := range v.NumField() {
				field := v.Type().Field(i)
				if !field.IsExported() {
					continue
				}
				key := v.Type().String() + "." + field.Name
				seen[key] = seen[key] || !v.Field(i).IsZero()
				walk(v.Field(i))
			}
		}
	}
	walk(reflect.ValueOf(fullSnapshot()))
	for key, set := range seen {
		if !set {
			t.Errorf("fixture leaves %s zero in every occurrence: set it, and add it to the wire shape", key)
		}
	}
}
