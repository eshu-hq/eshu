// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/goldengate"
)

func testResolver(t *testing.T) (ArtifactResolver, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "testdata/cassettes/awscloud"), 0o750); err != nil {
		t.Fatalf("mkdir cassette: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "fixture.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	snap := goldengate.Snapshot{
		SchemaVersion: "1",
		Graph: goldengate.GraphSnapshot{
			RequiredCorrelations: []goldengate.RequiredCorrelation{{ID: "rc-1"}},
		},
		QueryShapes: goldengate.QueryShapes{
			HTTP: map[string]goldengate.QueryShape{"repo_summary": {}},
			MCP:  map[string]goldengate.QueryShape{"get_repo_summary": {}},
		},
	}
	return ArtifactResolver{RepoRoot: root, Snapshot: snap}, root
}

func TestResolveCassetteAndFixturePaths(t *testing.T) {
	r, _ := testResolver(t)
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioCassette, Ref: "testdata/cassettes/awscloud"}); !ok {
		t.Error("present cassette dir should resolve")
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioParserFixture, Ref: "fixture.json"}); !ok {
		t.Error("present fixture file should resolve")
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioCassette, Ref: "testdata/cassettes/absent"}); ok {
		t.Error("missing cassette dir must not resolve")
	}
}

func TestResolveSnapshotBackedScenarios(t *testing.T) {
	r, _ := testResolver(t)
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioCorrelation, Ref: "rc-1"}); !ok {
		t.Error("rc-1 present in snapshot should resolve")
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioCorrelation, Ref: "rc-99"}); ok {
		t.Error("rc-99 absent from snapshot must not resolve")
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioAPIMCPGolden, Ref: "repo_summary"}); !ok {
		t.Error("http query shape should resolve")
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioAPIMCPGolden, Ref: "get_repo_summary"}); !ok {
		t.Error("mcp query shape should resolve")
	}
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioAPIMCPGolden, Ref: "missing_shape"}); ok {
		t.Error("absent query shape must not resolve")
	}
}

func TestResolveRejectsPathEscape(t *testing.T) {
	r, _ := testResolver(t)
	// A ref that escapes the repo root must not resolve, even if the target
	// happens to exist on disk: coverage artifacts live inside the repo.
	if ok, _ := r.Resolve(CoverageEntry{Scenario: ScenarioCassette, Ref: "../../etc/hosts"}); ok {
		t.Error("path-escaping ref must not resolve")
	}
}
