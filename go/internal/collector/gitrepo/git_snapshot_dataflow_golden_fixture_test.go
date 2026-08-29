// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitrepo

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestGoldenDataflowFixtureReachesCollectorReadContracts(t *testing.T) {
	t.Parallel()

	fixtureRoot, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "tests", "fixtures", "ecosystems", "go_comprehensive"))
	if err != nil {
		t.Fatalf("resolve golden dataflow fixture: %v", err)
	}
	snapshot, err := (NativeRepositorySnapshotter{
		EmitDataflow: true,
		Now: func() time.Time {
			return time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
		},
	}).SnapshotRepository(context.Background(), SelectedRepository{RepoPath: fixtureRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v", err)
	}

	var function *DataflowFunctionSnapshot
	for i := range snapshot.DataflowFunctions {
		candidate := &snapshot.DataflowFunctions[i]
		if candidate.RelativePath == "dataflow_proof.go" && candidate.FunctionName == "GoldenDataflowHandler" {
			function = candidate
			break
		}
	}
	if function == nil {
		t.Fatal("GoldenDataflowHandler dataflow function is absent")
	}
	if function.Language != "go" || function.LineNumber != 12 {
		t.Fatalf("function identity = %+v, want go dataflow_proof.go:12", function)
	}
	if got, want := len(function.CFGBlocks), 3; got != want {
		t.Errorf("CFG blocks = %d, want %d", got, want)
	}
	if got, want := len(function.CFGEdges), 3; got != want {
		t.Errorf("CFG edges = %d, want %d", got, want)
	}
	if got, want := len(function.DefUse), 4; got != want {
		t.Errorf("def-use edges = %d, want %d", got, want)
	}
	// Pin the rows, not just the count. The B-7 golden snapshot's
	// dispatch_reaching_def shape asserts these same four bindings and their
	// definition and use lines, so a silent change here would otherwise show up
	// only as a live gate failure hours later (eshu-hq/eshu#6090).
	wantDefUse := []map[string]any{
		{"binding": "r", "def_stmt": 0, "def_line": 12, "use_stmt": 1, "use_line": 13},
		{"binding": "query", "def_stmt": 1, "def_line": 13, "use_stmt": 2, "use_line": 14},
		{"binding": "db", "def_stmt": 0, "def_line": 12, "use_stmt": 3, "use_line": 15},
		{"binding": "query", "def_stmt": 1, "def_line": 13, "use_stmt": 3, "use_line": 15},
	}
	if !reflect.DeepEqual(function.DefUse, wantDefUse) {
		t.Errorf("def-use edges = %#v, want %#v", function.DefUse, wantDefUse)
	}
	if got, want := len(function.ControlDependencies), 1; got != want {
		t.Errorf("control dependencies = %d, want %d", got, want)
	}
	wantControlDependencies := []map[string]any{{
		"guard_block":     0,
		"guard_stmt":      2,
		"guard_line":      14,
		"guard":           "query != <literal>",
		"dependent_block": 2,
	}}
	if !reflect.DeepEqual(function.ControlDependencies, wantControlDependencies) {
		t.Errorf("control dependencies = %#v, want %#v", function.ControlDependencies, wantControlDependencies)
	}
	if function.Overflow || function.OverflowReason != "" {
		t.Errorf("function unexpectedly overflowed: %+v", function)
	}

	var positive []TaintEvidenceSnapshot
	for _, evidence := range snapshot.TaintEvidence {
		if evidence.RelativePath == "dataflow_proof.go" && evidence.FunctionName == "GoldenDataflowHandler" {
			positive = append(positive, evidence)
		}
		if evidence.RelativePath == "dataflow_proof.go" && evidence.FunctionName == "GoldenSafeQuery" {
			t.Errorf("safe constant query produced taint evidence: %+v", evidence)
		}
	}
	if len(positive) != 1 {
		t.Fatalf("GoldenDataflowHandler taint findings = %d, want 1: %+v", len(positive), positive)
	}
	finding := positive[0]
	if finding.Kind != "TAINTED" || finding.SourceKind != "http_request" || finding.SinkKind != "sql" || finding.Binding != "query" {
		t.Errorf("taint classification = %+v, want TAINTED http_request -> sql on query", finding)
	}
	if finding.SourceLine != 13 || finding.SinkLine != 15 || finding.Confidence != 0.8 {
		t.Errorf("taint provenance = %+v, want source line 13, sink line 15, confidence 0.8", finding)
	}
	if finding.GuardReason != "query != <literal>" {
		t.Errorf("taint guard reason = %q, want %q", finding.GuardReason, "query != <literal>")
	}
	if finding.FunctionUID == "" {
		t.Error("taint finding did not resolve the real Function entity")
	}
}
