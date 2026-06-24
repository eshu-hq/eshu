// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestVulnScanRepoFixtureCorpusHasParserBackedDependencyEvidence(t *testing.T) {
	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	registry := parser.DefaultRegistry()

	for _, tc := range vulnScanFixtureCorpusCases() {
		tc := tc
		if !vulnScanFixtureCorpusShouldHaveParserDependencyEvidence(tc) {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			root := filepath.Join("testdata", "vuln_scan_repo_fixtures", tc.fixture)
			dependencyRows := 0
			parsedFiles := 0
			for _, rel := range tc.expectedFiles {
				path := filepath.Join(root, rel)
				if _, ok := registry.LookupByPath(path); !ok {
					continue
				}
				payload, err := engine.ParsePath(root, path, false, parser.Options{})
				if err != nil {
					t.Fatalf("ParsePath(%s) error = %v, want nil", rel, err)
				}
				parsedFiles++
				for _, row := range parserVariableRows(payload) {
					if row["config_kind"] == "dependency" {
						dependencyRows++
					}
				}
			}
			if parsedFiles == 0 {
				t.Fatalf("fixture %q has no parser-registered files in %#v", tc.fixture, tc.expectedFiles)
			}
			if dependencyRows == 0 {
				t.Fatalf("fixture %q parsed %d files but emitted no dependency rows", tc.fixture, parsedFiles)
			}
		})
	}
}

func TestVulnScanRepoFixtureCorpusDoesNotCommitPrivateOrProviderStrings(t *testing.T) {
	root := filepath.Join("testdata", "vuln_scan_repo_fixtures")
	for _, tc := range vulnScanFixtureCorpusCases() {
		tc := tc
		t.Run(tc.fixture, func(t *testing.T) {
			for _, rel := range tc.expectedFiles {
				path := filepath.Join(root, tc.fixture, rel)
				contents, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("ReadFile(%s) error = %v, want nil", rel, err)
				}
				for _, forbidden := range []string{
					"registry.npmjs.org",
					"github.com/advisories",
					"rubygems.org",
					"private.example",
					"ghp_",
					"github_pat_",
					"glpat-",
				} {
					if strings.Contains(string(contents), forbidden) {
						t.Fatalf("fixture %q file %q contains forbidden term %q", tc.fixture, rel, forbidden)
					}
				}
			}
		})
	}
}

func vulnScanFixtureCorpusShouldHaveParserDependencyEvidence(tc vulnScanFixtureCorpusCase) bool {
	switch tc.state {
	case "malformed", "unsupported":
		return false
	}
	switch tc.manager {
	case "apk", "dpkg", "rpm":
		return false
	default:
		return true
	}
}

func parserVariableRows(payload map[string]any) []map[string]any {
	if rows, ok := payload["variables"].([]map[string]any); ok {
		return rows
	}
	return nil
}
