// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
	"gopkg.in/yaml.v3"
)

// TestRecoveryInputTriggers checks single-file edits to executable recovery
// inputs against both registry selection and the hosted workflow path filter.
func TestRecoveryInputTriggers(t *testing.T) {
	root := repoRoot(t)
	registry, err := cigates.Load(filepath.Join(root, "specs/ci-gates.v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		gate, workflow string
		paths          []string
	}{
		{"golden-corpus-gate", "golden-corpus-gate.yml", []string{
			"scripts/test-golden-documentation-section-reseed.sh",
			"scripts/fixtures/golden-documentation-reseed-schema.sql",
		}},
		{"golden-corpus-gate-neo4j", "golden-corpus-gate.yml", []string{
			"scripts/test-golden-documentation-section-reseed.sh",
			"scripts/fixtures/golden-documentation-reseed-schema.sql",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.gate, func(t *testing.T) {
			var triggers []string
			for _, gate := range registry.Gates {
				if gate.ID == tt.gate {
					triggers = gate.Triggers
				}
			}
			raw, err := os.ReadFile(filepath.Join(root, ".github/workflows", tt.workflow))
			if err != nil {
				t.Fatal(err)
			}
			var workflow struct {
				On struct {
					PullRequest struct {
						Paths []string `yaml:"paths"`
					} `yaml:"pull_request"`
				} `yaml:"on"`
			}
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				t.Fatal(err)
			}
			for _, path := range tt.paths {
				if !recoveryPathMatches(triggers, path) {
					t.Errorf("registry gate omits single-file change %s", path)
				}
				if !recoveryPathMatches(workflow.On.PullRequest.Paths, path) {
					t.Errorf("hosted workflow omits single-file change %s", path)
				}
			}
		})
	}
}

// recoveryPathMatches uses the registry's actual path matcher for each input.
func recoveryPathMatches(patterns []string, path string) bool {
	for _, pattern := range patterns {
		if cigates.MatchGlob(pattern, path) {
			return true
		}
	}
	return false
}
