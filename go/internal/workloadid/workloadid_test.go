// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workloadid

import (
	"reflect"
	"testing"
)

// These tests pin the CURRENT identifier format on purpose. This step changes
// no identifier value — it only makes construction enumerable by the compiler
// ahead of the re-key (#5385). Pinning the current format means the re-key
// commit has to update these assertions deliberately, rather than the format
// moving as an unnoticed side effect.

func TestNewWorkloadIDPinsCurrentFormat(t *testing.T) {
	cases := []struct {
		name     string
		repoID   string
		workload string
		want     string
	}{
		{"plain name", "repo:alpha", "checkout", "workload:checkout"},
		{"repository is not yet part of the key", "repo:beta", "checkout", "workload:checkout"},
		{"name with a hyphen", "repo:alpha", "api-gateway", "workload:api-gateway"},
		{"surrounding whitespace is trimmed", "repo:alpha", "  checkout  ", "workload:checkout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewWorkloadID(tc.repoID, tc.workload).String(); got != tc.want {
				t.Fatalf("NewWorkloadID(%q, %q) = %q, want %q", tc.repoID, tc.workload, got, tc.want)
			}
		})
	}
}

// Two repositories with the same workload name produce the same id today. That
// is the defect #5385 exists to fix, and pinning it here means the re-key
// cannot land without this assertion being changed on purpose.
func TestNewWorkloadIDCollidesAcrossRepositories(t *testing.T) {
	alpha := NewWorkloadID("repo:alpha", "checkout")
	beta := NewWorkloadID("repo:beta", "checkout")
	if alpha != beta {
		t.Fatalf("expected the current name-only key to collide, got %q vs %q", alpha, beta)
	}
}

func TestNewWorkloadInstanceIDPinsCurrentFormat(t *testing.T) {
	got := NewWorkloadInstanceID("repo:alpha", "checkout", "production").String()
	if want := "workload-instance:checkout:production"; got != want {
		t.Fatalf("NewWorkloadInstanceID = %q, want %q", got, want)
	}
}

func TestNewWorkloadInstanceIDCollidesAcrossRepositories(t *testing.T) {
	alpha := NewWorkloadInstanceID("repo:alpha", "checkout", "production")
	beta := NewWorkloadInstanceID("repo:beta", "checkout", "production")
	if alpha != beta {
		t.Fatalf("expected the current instance key to collide, got %q vs %q", alpha, beta)
	}
}

// The typed value must remain usable wherever a string is required, or callers
// will cast at every site and the type will stop being load-bearing.
func TestWorkloadIDStringRoundTrip(t *testing.T) {
	if got := NewWorkloadID("repo:alpha", "checkout").String(); got != "workload:checkout" {
		t.Fatalf("String() = %q", got)
	}
	if got := (WorkloadID{}).String(); got != "" {
		t.Fatalf("empty id must stringify to empty, got %q", got)
	}
	if got := NewWorkloadInstanceID("repo:alpha", "checkout", "production").String(); got != "workload-instance:checkout:production" {
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

// A blank segment must not produce a bare prefix or an id with an empty
// segment: either would MERGE unrelated candidates onto one shared node.
func TestConstructorsRejectBlankSegments(t *testing.T) {
	cases := []struct {
		name string
		got  string
	}{
		{"blank workload name", NewWorkloadID("repo:alpha", "   ").String()},
		{"empty workload name", NewWorkloadID("repo:alpha", "").String()},
		{"blank environment", NewWorkloadInstanceID("repo:alpha", "checkout", "  ").String()},
		{"blank name on instance", NewWorkloadInstanceID("repo:alpha", "", "production").String()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != "" {
				t.Fatalf("expected empty id, got %q", tc.got)
			}
		})
	}
}
