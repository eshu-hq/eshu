// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workloadid

import (
	"reflect"
	"testing"
)

// These tests pin the repository-scoped identifier format (#5385 Option C):
// workload:<repo_id>:<name> and workload-instance:<repo_id>:<name>:<environment>.
// Two repositories with the same workload name produce distinct ids; the
// name-only key that collapsed them onto one node is gone.
func TestNewWorkloadIDEmitsScopedFormat(t *testing.T) {
	cases := []struct {
		name     string
		repoID   string
		workload string
		want     string
	}{
		{"plain name", "repo:alpha", "checkout", "workload:repo:alpha:checkout"},
		{"repository scopes the key", "repo:beta", "checkout", "workload:repo:beta:checkout"},
		{"name with a hyphen", "repo:alpha", "api-gateway", "workload:repo:alpha:api-gateway"},
		{"surrounding whitespace is trimmed", "repo:alpha", "  checkout  ", "workload:repo:alpha:checkout"},
		{"repo id whitespace is trimmed", "  repo:alpha  ", "checkout", "workload:repo:alpha:checkout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewWorkloadID(tc.repoID, tc.workload).String(); got != tc.want {
				t.Fatalf("NewWorkloadID(%q, %q) = %q, want %q", tc.repoID, tc.workload, got, tc.want)
			}
		})
	}
}

// Same-named workloads in distinct repositories must not share an id: the
// cross-repo merge this re-key exists to remove.
func TestNewWorkloadIDIsolatesAcrossRepositories(t *testing.T) {
	alpha := NewWorkloadID("repo:alpha", "checkout")
	beta := NewWorkloadID("repo:beta", "checkout")
	if alpha == beta {
		t.Fatalf("expected distinct scoped keys, both = %q", alpha)
	}
}

func TestNewWorkloadInstanceIDEmitsScopedFormat(t *testing.T) {
	got := NewWorkloadInstanceID("repo:alpha", "checkout", "production").String()
	if want := "workload-instance:repo:alpha:checkout:production"; got != want {
		t.Fatalf("NewWorkloadInstanceID = %q, want %q", got, want)
	}
}

func TestNewWorkloadInstanceIDIsolatesAcrossRepositories(t *testing.T) {
	alpha := NewWorkloadInstanceID("repo:alpha", "checkout", "production")
	beta := NewWorkloadInstanceID("repo:beta", "checkout", "production")
	if alpha == beta {
		t.Fatalf("expected distinct scoped instance keys, both = %q", alpha)
	}
}

// The typed value must remain usable wherever a string is required, or callers
// will cast at every site and the type will stop being load-bearing.
func TestWorkloadIDStringRoundTrip(t *testing.T) {
	if got := NewWorkloadID("repo:alpha", "checkout").String(); got != "workload:repo:alpha:checkout" {
		t.Fatalf("String() = %q", got)
	}
	if got := (WorkloadID{}).String(); got != "" {
		t.Fatalf("empty id must stringify to empty, got %q", got)
	}
	if got := NewWorkloadInstanceID("repo:alpha", "checkout", "production").String(); got != "workload-instance:repo:alpha:checkout:production" {
		t.Fatalf("instance String() = %q", got)
	}
}

// The identifier types must stay opaque: every field unexported, so no
// caller can convert or composite-literal its way around the constructors.
// Without this, a re-key that changes NewWorkloadID would leave hand-built
// values compiling on the old format (#6580 codex P1).
func TestIdentifierTypesAreOpaque(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeOf(WorkloadID{}), reflect.TypeOf(WorkloadInstanceID{})} {
		if typ.Kind() != reflect.Struct {
			t.Fatalf("%s must be a struct, got %s", typ.Name(), typ.Kind())
		}
		for i := 0; i < typ.NumField(); i++ {
			if field := typ.Field(i); field.PkgPath == "" {
				t.Fatalf("%s.%s must stay unexported", typ.Name(), field.Name)
			}
		}
	}
}

// A name or environment containing a colon is rejected: the readable
// composite has no parser, and a colon inside a segment would make the key
// ambiguous to a human reader (compatibility doc decision "Colon rejection").
// The repository segment is exempt: canonical repo ids carry their own prefix
// (repository:r_<hex>), so the scoped key embeds colons by construction.
func TestConstructorsRejectColonSegments(t *testing.T) {
	cases := []struct {
		name string
		got  string
	}{
		{"colon in workload name", NewWorkloadID("repo:alpha", "check:out").String()},
		{"colon in instance name", NewWorkloadInstanceID("repo:alpha", "check:out", "prod").String()},
		{"colon in environment", NewWorkloadInstanceID("repo:alpha", "checkout", "pro:d").String()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != "" {
				t.Fatalf("expected empty id, got %q", tc.got)
			}
		})
	}
	if got := NewWorkloadID("repository:r_a", "checkout").String(); got != "workload:repository:r_a:checkout" {
		t.Fatalf("repo-id colons are structural, got %q", got)
	}
}

// A blank segment must not produce a bare prefix or an id with an empty
// segment: either would MERGE unrelated candidates onto one shared node.
// A blank repository fails closed to the empty id for the same reason: the
// repository is the scoping segment, so an unscoped key is a collision.
func TestConstructorsRejectBlankSegments(t *testing.T) {
	cases := []struct {
		name string
		got  string
	}{
		{"blank workload name", NewWorkloadID("repo:alpha", "   ").String()},
		{"empty workload name", NewWorkloadID("repo:alpha", "").String()},
		{"blank repository", NewWorkloadID("   ", "checkout").String()},
		{"empty repository", NewWorkloadID("", "checkout").String()},
		{"blank environment", NewWorkloadInstanceID("repo:alpha", "checkout", "  ").String()},
		{"blank name on instance", NewWorkloadInstanceID("repo:alpha", "", "production").String()},
		{"blank repository on instance", NewWorkloadInstanceID("", "checkout", "production").String()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != "" {
				t.Fatalf("expected empty id, got %q", tc.got)
			}
		})
	}
}

// TestClassifyRejections pins the tally attribution: blank segments group
// under blank_segment, a colon-bearing name under colon_in_name, and a
// colon-bearing environment under colon_in_environment. The emission test in
// the reducer package proves these labels reach the counter verbatim.
func TestClassifyRejections(t *testing.T) {
	t.Parallel()
	if got := ClassifyWorkloadRejection("", "checkout"); got != RejectionBlankSegment {
		t.Errorf("ClassifyWorkloadRejection blank repo = %q, want blank_segment", got)
	}
	if got := ClassifyWorkloadRejection("repo:alpha", ""); got != RejectionBlankSegment {
		t.Errorf("ClassifyWorkloadRejection blank name = %q, want blank_segment", got)
	}
	if got := ClassifyWorkloadRejection("repo:alpha", "api:v2"); got != RejectionColonInName {
		t.Errorf("ClassifyWorkloadRejection colon name = %q, want colon_in_name", got)
	}
	if got := ClassifyInstanceRejection("repo:alpha", "checkout", ""); got != RejectionBlankSegment {
		t.Errorf("ClassifyInstanceRejection blank env = %q, want blank_segment", got)
	}
	if got := ClassifyInstanceRejection("repo:alpha", "api:v2", "prod"); got != RejectionColonInName {
		t.Errorf("ClassifyInstanceRejection colon name = %q, want colon_in_name", got)
	}
	if got := ClassifyInstanceRejection("repo:alpha", "checkout", "us:east"); got != RejectionColonInEnvironment {
		t.Errorf("ClassifyInstanceRejection colon env = %q, want colon_in_environment", got)
	}
}

// TestRejectionTallyNoteNilSafe pins the nil-tally contract read-only row
// rebuilds rely on: notes are discarded, never panicking.
func TestRejectionTallyNoteNilSafe(t *testing.T) {
	t.Parallel()
	var tally *RejectionTally
	tally.Note(RejectionBlankSegment)
	tally.Note(RejectionColonInName)
	tally.Note(RejectionColonInEnvironment)
	var zero RejectionTally
	if !zero.Empty() {
		t.Error("zero RejectionTally Empty() = false, want true")
	}
	zero.Note(RejectionBlankSegment)
	if zero.Empty() || zero.BlankSegment != 1 {
		t.Errorf("noted tally = %+v, want one blank_segment", zero)
	}
}
