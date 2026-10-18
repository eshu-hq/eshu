// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeModuleIdentityGraph answers the two #8168 Module identity reads.
type fakeModuleIdentityGraph struct {
	duplicates []string
	unkeyed    int64
	err        error
}

func (f fakeModuleIdentityGraph) ModuleIdentityViolations(context.Context) ([]string, int64, error) {
	return f.duplicates, f.unkeyed, f.err
}

func moduleIdentityFinding(t *testing.T, r Report) Finding {
	t.Helper()
	for _, f := range r.Findings {
		if f.Phase == "graph" && f.Check == "module_identity" {
			return f
		}
	}
	t.Fatalf("no graph/module_identity finding in %+v", r.Findings)
	return Finding{}
}

// GREEN: a replayed graph with one keyed canonical Module per (name, lang)
// and no unkeyed canonical Module passes the required check.
func TestModuleIdentityCleanReplayPasses(t *testing.T) {
	var r Report
	checkModuleIdentity(context.Background(), fakeModuleIdentityGraph{}, true, &r)
	if f := moduleIdentityFinding(t, r); !f.OK || !f.Required {
		t.Fatalf("clean replay finding = %+v, want required pass", f)
	}
}

// RED: a duplicated (name, lang), or an unkeyed canonical Module, fails the
// leg and the detail names what broke.
func TestModuleIdentityPlantedViolationsFail(t *testing.T) {
	for name, tc := range map[string]struct {
		graph fakeModuleIdentityGraph
		want  string
	}{
		"duplicate key":  {graph: fakeModuleIdentityGraph{duplicates: []string{"fmt@go x2"}}, want: "fmt@go x2"},
		"unkeyed module": {graph: fakeModuleIdentityGraph{unkeyed: 3}, want: "3 canonical Module nodes without import_key"},
	} {
		var r Report
		checkModuleIdentity(context.Background(), tc.graph, true, &r)
		f := moduleIdentityFinding(t, r)
		if f.OK || !f.Required || !strings.Contains(f.Detail, tc.want) {
			t.Errorf("%s: finding = %+v, want a required failure naming %q", name, f, tc.want)
		}
	}
}

func TestModuleIdentityFailsClosedWhenItCannotRun(t *testing.T) {
	var r Report
	checkModuleIdentity(context.Background(), fakeModuleIdentityGraph{err: errors.New("bolt down")}, true, &r)
	if f := moduleIdentityFinding(t, r); f.OK || !f.Required {
		t.Fatalf("a check that could not run = %+v, want a required failure", f)
	}
}

// The check is scoped to the Neo4j leg, the backend #8168 measured and the
// ruling named; off Neo4j it records a non-required skip that says why.
func TestModuleIdentitySkippedOffNeo4j(t *testing.T) {
	var r Report
	checkModuleIdentity(context.Background(), fakeModuleIdentityGraph{unkeyed: 1}, false, &r)
	f := moduleIdentityFinding(t, r)
	if !f.OK || f.Required || !strings.Contains(f.Detail, "Neo4j") {
		t.Fatalf("off-Neo4j finding = %+v, want a non-required skip naming Neo4j", f)
	}
}
