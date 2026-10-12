// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeCoverageFixture(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadManifestFileOrderedSourceCoverageFragments(t *testing.T) {
	dir := t.TempDir()
	root := writeCoverageFixture(t, dir, "query-source-coverage.yaml", "version: 1\ngrandfathered_non_hot_baseline: frozen\nsource_coverage_files:\n  - source-coverage/first.yaml\n  - source-coverage/second.yaml\n")
	writeCoverageFixture(t, dir, "source-coverage/first.yaml", "version: 1\nsource_coverage:\n  - file: shared.go\n    calls: [{symbol: first, count: 1}]\n")
	writeCoverageFixture(t, dir, "source-coverage/second.yaml", "version: 1\nsource_coverage:\n  - file: shared.go\n    calls: [{symbol: second, count: 1}]\n")
	manifest, err := LoadManifestFile(root)
	if err != nil {
		t.Fatalf("LoadManifestFile() error = %v", err)
	}
	if manifest.GrandfatheredNonHotBaseline != "frozen" {
		t.Fatalf("baseline = %q", manifest.GrandfatheredNonHotBaseline)
	}
	want := []SourceCoverage{
		{File: "shared.go", Calls: []QueryCallsite{{Symbol: "first", Count: 1}}},
		{File: "shared.go", Calls: []QueryCallsite{{Symbol: "second", Count: 1}}},
	}
	if !reflect.DeepEqual(manifest.SourceCoverage, want) {
		t.Fatalf("coverage = %#v, want %#v", manifest.SourceCoverage, want)
	}
}

func TestLoadManifestFileRejectsUnsafeSourceCoverageFragments(t *testing.T) {
	const validShard = "version: 1\nsource_coverage:\n  - file: shared.go\n    calls: [{symbol: first, count: 1}]\n"
	for _, test := range []struct {
		name   string
		root   string
		first  string
		second string
	}{
		{"mixed flat and files", "source_coverage_files: [source-coverage/first.yaml]\nsource_coverage:\n  - file: inline.go\n    calls: [{symbol: inline, count: 1}]\n", validShard, ""},
		{"empty reference", "source_coverage_files: [\"\"]\n", validShard, ""},
		{"duplicate reference", "source_coverage_files: [source-coverage/first.yaml, source-coverage/first.yaml]\n", validShard, ""},
		{"missing fragment", "source_coverage_files: [source-coverage/missing.yaml]\n", validShard, ""},
		{"absolute reference", "source_coverage_files: [/tmp/first.yaml]\n", validShard, ""},
		{"parent traversal", "source_coverage_files: [../first.yaml]\n", validShard, ""},
		{"unclean reference", "source_coverage_files: [source-coverage/./first.yaml]\n", validShard, ""},
		{"nested reference", "source_coverage_files: [source-coverage/first.yaml]\n", "version: 1\nsource_coverage_files: [source-coverage/second.yaml]\n", ""},
		{"unknown shard field", "source_coverage_files: [source-coverage/first.yaml]\n", validShard + "unknown: true\n", ""},
		{"unknown root field", "source_coverage_files: [source-coverage/first.yaml]\nunknown: true\n", validShard, ""},
		{"malformed shard", "source_coverage_files: [source-coverage/first.yaml]\n", "version: 1\nsource_coverage: [", ""},
		{"multiple shard documents", "source_coverage_files: [source-coverage/first.yaml]\n", validShard + "---\nversion: 1\nsource_coverage: []\n", ""},
		{"wrong shard version", "source_coverage_files: [source-coverage/first.yaml]\n", strings.Replace(validShard, "version: 1", "version: 2", 1), ""},
		{"duplicate shard key", "source_coverage_files: [source-coverage/first.yaml]\n", validShard + "source_coverage: []\n", ""},
		{"duplicate root key", "source_coverage_files: [source-coverage/first.yaml]\nsource_coverage_files: []\n", validShard, ""},
		{"empty fragment", "source_coverage_files: [source-coverage/first.yaml]\n", "version: 1\nsource_coverage: []\n", ""},
		{"duplicate call identity", "source_coverage_files: [source-coverage/first.yaml, source-coverage/second.yaml]\n", validShard, validShard},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			root := writeCoverageFixture(t, dir, "query-source-coverage.yaml", "version: 1\n"+test.root)
			if test.first != "" {
				writeCoverageFixture(t, dir, "source-coverage/first.yaml", test.first)
			}
			if test.second != "" {
				writeCoverageFixture(t, dir, "source-coverage/second.yaml", test.second)
			}
			if _, err := LoadManifestFile(root); err == nil {
				t.Fatal("LoadManifestFile() accepted invalid source coverage fragments")
			}
		})
	}
}

func TestLoadManifestFileRejectsEscapingSourceCoverageSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := writeCoverageFixture(t, t.TempDir(), "outside.yaml", "version: 1\nsource_coverage:\n  - file: outside.go\n    calls: [{symbol: read, count: 1}]\n")
	root := writeCoverageFixture(t, dir, "query-source-coverage.yaml", "version: 1\nsource_coverage_files: [source-coverage/first.yaml]\n")
	if err := os.MkdirAll(filepath.Join(dir, "source-coverage"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "source-coverage/first.yaml")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := LoadManifestFile(root); err == nil {
		t.Fatal("LoadManifestFile() followed source coverage symlink outside manifest directory")
	}
}

func TestLoadManifestFileRejectsMissingCoverageForFrozenBaseline(t *testing.T) {
	root := writeCoverageFixture(t, t.TempDir(), "query-source-coverage.yaml", "version: 1\ngrandfathered_non_hot_baseline: frozen\n")
	if _, err := LoadManifestFile(root); err == nil {
		t.Fatal("LoadManifestFile() accepted frozen source coverage without rows or references")
	}
}

func TestLoadManifestFileKeepsFlatSourceCoverage(t *testing.T) {
	root := writeCoverageFixture(t, t.TempDir(), "query-source-coverage.yaml", "version: 1\nsource_coverage:\n  - file: flat.go\n    calls: [{symbol: read, count: 1}]\n")
	manifest, err := LoadManifestFile(root)
	if err != nil {
		t.Fatalf("LoadManifestFile(flat) error = %v", err)
	}
	if len(manifest.SourceCoverage) != 1 || manifest.SourceCoverage[0].File != "flat.go" {
		t.Fatalf("flat source coverage = %#v", manifest.SourceCoverage)
	}
}
