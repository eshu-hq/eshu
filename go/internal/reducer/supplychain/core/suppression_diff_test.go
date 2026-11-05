// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

func TestChangedSuppressionCVEIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rows []suppressionDiffRow
		want []string
	}{
		{name: "no diff rows reopens nobody", rows: nil, want: nil},
		{
			name: "added suppression names its new id",
			rows: []suppressionDiffRow{{SuppressionID: "s1", CurrentCVEID: "CVE-2026-1"}},
			want: []string{"CVE-2026-1"},
		},
		{
			name: "removed suppression names its old id",
			rows: []suppressionDiffRow{{SuppressionID: "s1", PriorCVEID: "CVE-2026-1"}},
			want: []string{"CVE-2026-1"},
		},
		{
			name: "edit that keeps the id names it once",
			rows: []suppressionDiffRow{{SuppressionID: "s1", CurrentCVEID: "CVE-2026-1", PriorCVEID: "CVE-2026-1"}},
			want: []string{"CVE-2026-1"},
		},
		{
			name: "re-scope names both ids so the old holders drop it",
			rows: []suppressionDiffRow{{SuppressionID: "s1", CurrentCVEID: "CVE-2026-2", PriorCVEID: "CVE-2026-1"}},
			want: []string{"CVE-2026-1", "CVE-2026-2"},
		},
		{
			name: "scope with no cve id adds nothing",
			rows: []suppressionDiffRow{{SuppressionID: "s1"}},
			want: nil,
		},
		{
			name: "blank and whitespace ids drop, decode trims the same way",
			rows: []suppressionDiffRow{
				{SuppressionID: "s1", CurrentCVEID: "   ", PriorCVEID: "\t"},
				{SuppressionID: "s2", CurrentCVEID: " CVE-2026-3 "},
			},
			want: []string{"CVE-2026-3"},
		},
		{
			name: "lower case also names the upper form the exact holder read needs",
			rows: []suppressionDiffRow{{SuppressionID: "s1", CurrentCVEID: "cve-2026-4"}},
			want: []string{"CVE-2026-4", "cve-2026-4"},
		},
		{
			name: "many suppressions on one id collapse and sort",
			rows: []suppressionDiffRow{
				{SuppressionID: "s3", CurrentCVEID: "CVE-2026-9"},
				{SuppressionID: "s1", CurrentCVEID: "CVE-2026-5"},
				{SuppressionID: "s2", PriorCVEID: "CVE-2026-9"},
			},
			want: []string{"CVE-2026-5", "CVE-2026-9"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := changedSuppressionCVEIDs(tc.rows)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("changedSuppressionCVEIDs() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestCountSuppressionsWithoutCVEScope(t *testing.T) {
	t.Parallel()

	rows := []suppressionDiffRow{
		{SuppressionID: "advisory-only"},
		{SuppressionID: "blank", CurrentCVEID: " ", PriorCVEID: "\t"},
		{SuppressionID: "named", CurrentCVEID: "CVE-2026-1"},
		{SuppressionID: "dropped-its-id", PriorCVEID: "CVE-2026-2"},
	}
	if got, want := countSuppressionsWithoutCVEScope(rows), 2; got != want {
		t.Fatalf("countSuppressionsWithoutCVEScope() = %d, want %d", got, want)
	}
}

func TestUnionSuppressionFanOutCVEIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		changed []string
		rows    []suppressionDiffRow
		want    []string
	}{
		{name: "unchanged set keeps the empty fan-out", changed: nil, rows: nil, want: []string{}},
		{
			name:    "a pass with no suppression change keeps its own set",
			changed: []string{"CVE-2026-1"},
			want:    []string{"CVE-2026-1"},
		},
		{
			name: "a suppression-only change alone sets the fan-out",
			rows: []suppressionDiffRow{{SuppressionID: "s1", CurrentCVEID: "CVE-2026-2"}},
			want: []string{"CVE-2026-2"},
		},
		{
			name:    "overlap collapses and the result stays sorted",
			changed: []string{"CVE-2026-3", "CVE-2026-1"},
			rows:    []suppressionDiffRow{{SuppressionID: "s1", CurrentCVEID: "CVE-2026-1", PriorCVEID: "CVE-2026-2"}},
			want:    []string{"CVE-2026-1", "CVE-2026-2", "CVE-2026-3"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := unionSuppressionFanOutCVEIDs(tc.changed, tc.rows)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("unionSuppressionFanOutCVEIDs() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestSelectSuppressionDiffBaseline(t *testing.T) {
	t.Parallel()

	at := func(hour int) time.Time { return time.Date(2026, 11, 4, hour, 0, 0, 0, time.UTC) }
	gen := func(id string, activatedHour int, admitted bool) suppressionGeneration {
		g := suppressionGeneration{GenerationID: id, Status: "superseded", IngestedAt: at(activatedHour), Admitted: admitted}
		if activatedHour > 0 {
			g.ActivatedAt = at(activatedHour)
		}
		return g
	}

	tests := []struct {
		name       string
		current    string
		candidates []suppressionGeneration
		want       suppressionDiffBaseline
	}{
		{
			name:       "plain chain takes the previous admitted generation",
			current:    "c",
			candidates: []suppressionGeneration{gen("a", 1, true), gen("b", 2, true), gen("c", 3, false)},
			want:       suppressionDiffBaseline{GenerationID: "b", Kind: suppressionBaselineAdmitted},
		},
		{
			name:       "coalesced generation: b was never admitted, so c reaches back to a",
			current:    "c",
			candidates: []suppressionGeneration{gen("a", 1, true), gen("b", 2, false), gen("c", 3, false)},
			want:       suppressionDiffBaseline{GenerationID: "a", Kind: suppressionBaselineAdmitted},
		},
		{
			name:       "stale pass of b after c committed ignores the newer generation",
			current:    "b",
			candidates: []suppressionGeneration{gen("a", 1, true), gen("b", 2, false), gen("c", 3, true)},
			want:       suppressionDiffBaseline{GenerationID: "a", Kind: suppressionBaselineAdmitted},
		},
		{
			name:    "a generation superseded while pending never counts, admitted or not",
			current: "c",
			candidates: []suppressionGeneration{
				gen("a", 1, true),
				{GenerationID: "ghost", Status: "superseded", IngestedAt: at(2), Admitted: true},
				gen("c", 3, false),
			},
			want: suppressionDiffBaseline{GenerationID: "a", Kind: suppressionBaselineAdmitted},
		},
		{
			name:    "a failed generation never counts",
			current: "c",
			candidates: []suppressionGeneration{
				gen("a", 1, true),
				{GenerationID: "bad", Status: "failed", ActivatedAt: at(2), IngestedAt: at(2), Admitted: true},
				gen("c", 3, false),
			},
			want: suppressionDiffBaseline{GenerationID: "a", Kind: suppressionBaselineAdmitted},
		},
		{
			name:       "nothing retained was admitted: oldest retained covers every retained change",
			current:    "c",
			candidates: []suppressionGeneration{gen("a", 1, false), gen("b", 2, false), gen("c", 3, false)},
			want:       suppressionDiffBaseline{GenerationID: "a", Kind: suppressionBaselineOldestRetained},
		},
		{
			name:       "first generation has no baseline, every suppression is new",
			current:    "a",
			candidates: []suppressionGeneration{gen("a", 1, false)},
			want:       suppressionDiffBaseline{Kind: suppressionBaselineNone},
		},
		{
			name:       "current generation never activated has no baseline",
			current:    "ghost",
			candidates: []suppressionGeneration{gen("a", 1, true), {GenerationID: "ghost", Status: "pending", IngestedAt: at(2)}},
			want:       suppressionDiffBaseline{Kind: suppressionBaselineNone},
		},
		{
			name:       "current generation missing from the list has no baseline",
			current:    "gone",
			candidates: []suppressionGeneration{gen("a", 1, true)},
			want:       suppressionDiffBaseline{Kind: suppressionBaselineNone},
		},
		{
			name:    "ingest tie falls to generation id",
			current: "c",
			candidates: []suppressionGeneration{
				{GenerationID: "x", Status: "superseded", ActivatedAt: at(2), IngestedAt: at(1), Admitted: true},
				{GenerationID: "z", Status: "superseded", ActivatedAt: at(2), IngestedAt: at(2), Admitted: true},
				{GenerationID: "y", Status: "superseded", ActivatedAt: at(2), IngestedAt: at(2), Admitted: true},
				gen("c", 3, false),
			},
			want: suppressionDiffBaseline{GenerationID: "z", Kind: suppressionBaselineAdmitted},
		},
		{
			// The sweep, the guard and the projector order generations by
			// (ingested_at, generation_id). b was ingested before c but its Ack
			// landed after c's, so activated_at order puts it on the wrong side.
			name:    "older means ingest order, not activation order",
			current: "c",
			candidates: []suppressionGeneration{
				gen("a", 1, true),
				{GenerationID: "b", Status: "superseded", IngestedAt: at(2), ActivatedAt: at(5), Admitted: true},
				{GenerationID: "c", Status: "active", IngestedAt: at(3), ActivatedAt: at(4)},
			},
			want: suppressionDiffBaseline{GenerationID: "b", Kind: suppressionBaselineAdmitted},
		},
		{
			name:    "a generation ingested after the current one is never its baseline, whatever its activation time",
			current: "c",
			candidates: []suppressionGeneration{
				gen("a", 1, true),
				{GenerationID: "c", Status: "superseded", IngestedAt: at(3), ActivatedAt: at(6)},
				{GenerationID: "d", Status: "active", IngestedAt: at(4), ActivatedAt: at(5), Admitted: true},
			},
			want: suppressionDiffBaseline{GenerationID: "a", Kind: suppressionBaselineAdmitted},
		},
		{
			name:    "oldest retained follows ingest order too",
			current: "c",
			candidates: []suppressionGeneration{
				{GenerationID: "a", Status: "superseded", IngestedAt: at(1), ActivatedAt: at(9)},
				{GenerationID: "b", Status: "superseded", IngestedAt: at(2), ActivatedAt: at(3)},
				{GenerationID: "c", Status: "active", IngestedAt: at(4), ActivatedAt: at(5)},
			},
			want: suppressionDiffBaseline{GenerationID: "a", Kind: suppressionBaselineOldestRetained},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := selectSuppressionDiffBaseline(tc.current, tc.candidates); got != tc.want {
				t.Fatalf("selectSuppressionDiffBaseline() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDecideSuppressionDiff(t *testing.T) {
	t.Parallel()

	suppression := []string{chain.VulnerabilitySuppressionFactKind}
	tests := []struct {
		name string
		pass suppressionDiffPass
		want suppressionDiffAction
	}{
		{
			name: "KEV scope never diffs",
			pass: suppressionDiffPass{ScopeFactKinds: []string{chain.VulnerabilityKnownExploitedFactKind}, GenerationStatus: "active", GenerationActivated: true},
			want: suppressionDiffSkipKind,
		},
		{
			name: "EPSS scope never diffs",
			pass: suppressionDiffPass{ScopeFactKinds: []string{chain.VulnerabilityEPSSScoreFactKind}, GenerationStatus: "active", GenerationActivated: true},
			want: suppressionDiffSkipKind,
		},
		{
			name: "advisory scope never diffs",
			pass: suppressionDiffPass{ScopeFactKinds: []string{chain.VulnerabilityCVEFactKind}, GenerationStatus: "active", GenerationActivated: true},
			want: suppressionDiffSkipKind,
		},
		{
			name: "no own facts never diffs",
			pass: suppressionDiffPass{GenerationStatus: "active", GenerationActivated: true},
			want: suppressionDiffSkipKind,
		},
		{
			name: "first pass of an active generation runs",
			pass: suppressionDiffPass{ScopeFactKinds: suppression, GenerationStatus: "active", GenerationActivated: true},
			want: suppressionDiffRun,
		},
		{
			name: "first pass of a superseded generation that was activated still runs, or its change is lost",
			pass: suppressionDiffPass{ScopeFactKinds: suppression, GenerationStatus: "superseded", GenerationActivated: true},
			want: suppressionDiffRun,
		},
		{
			name: "a pass before activation defers: holders would read the old set",
			pass: suppressionDiffPass{ScopeFactKinds: suppression, GenerationStatus: "pending"},
			want: suppressionDiffDeferUnactivated,
		},
		{
			name: "a generation superseded while pending was never readable",
			pass: suppressionDiffPass{ScopeFactKinds: suppression, GenerationStatus: "superseded"},
			want: suppressionDiffSkipNeverActivated,
		},
		{
			name: "a failed generation was never readable",
			pass: suppressionDiffPass{ScopeFactKinds: suppression, GenerationStatus: "failed"},
			want: suppressionDiffSkipNeverActivated,
		},
		{
			name: "duplicate delivery of an admitted pass reopens nobody",
			pass: suppressionDiffPass{ScopeFactKinds: suppression, GenerationStatus: "active", GenerationActivated: true, AlreadyAdmitted: true},
			want: suppressionDiffSkipDuplicate,
		},
		{
			name: "a retry that owes fan-out recomputes, finding rows cannot rebuild the set",
			pass: suppressionDiffPass{ScopeFactKinds: suppression, GenerationStatus: "active", GenerationActivated: true, AlreadyAdmitted: true, Recovery: true},
			want: suppressionDiffRun,
		},
		{
			name: "mixed own kinds still diff the suppression rows only",
			pass: suppressionDiffPass{
				ScopeFactKinds:      []string{chain.VulnerabilityKnownExploitedFactKind, chain.VulnerabilitySuppressionFactKind},
				GenerationStatus:    "active",
				GenerationActivated: true,
			},
			want: suppressionDiffRun,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := decideSuppressionDiff(tc.pass); got != tc.want {
				t.Fatalf("decideSuppressionDiff() = %q, want %q", got, tc.want)
			}
		})
	}
}
