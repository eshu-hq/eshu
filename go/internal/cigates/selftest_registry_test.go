// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cigates"
)

func TestCommittedRegistrySelfTestHarnessInputsAreCovered(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	registry, err := cigates.Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("load committed registry: %v", err)
	}

	cases := map[string][]string{
		"go-file-cap": {
			"scripts/lib/test-precommit-go-filecap-cases.sh",
		},
		"license-header": {
			"scripts/add-license-header.sh",
		},
		"agent-canon": {
			".pre-commit-config.yaml",
			"scripts/verify-no-ai-attribution.sh",
			"specs/ci-gates.v1.yaml",
		},
		"edge-source-tool-coverage": {
			"go/internal/reducer/cross_repo_evidence_type.go",
		},
		"scale-corpus-suite": {
			"scripts/lib/test-verify-scale-corpus-suite-missing-pathological.yaml",
		},
		"code-coverage-report": {
			".github/workflows/code-coverage-report.yml",
			"docs/mkdocs.yml",
			"docs/public/reference/code-coverage.md",
			"docs/public/reference/code-coverage-shield.json",
			"README.md",
			"specs/ci-gates.v1.yaml",
		},
		"ci-gate-registry": {
			"AGENTS.md",
			"CLAUDE.md",
			"CONTRIBUTING.md",
			"Makefile",
			"docs/public/contributing-language-support.md",
			"docs/public/guides/fixture-ecosystems.md",
			"docs/public/reference/local-testing/quick-verification-matrix.md",
			"docs/public/reference/local-testing/verification-gates.md",
			"docs/public/reference/ci-gates.md",
			"go/internal/parser/AGENTS.md",
			"scripts/dev/precommit-go.sh",
			"scripts/lib/pre-pr-fixture-consumers.sh",
			"scripts/lib/test-pre-pr-fixture-consumers.sh",
			"specs/product-claims.v1.yaml",
			"tests/run_tests.sh",
		},
		"golden-corpus-filter-exhaustive": {
			"go/cmd/golden-corpus-gate/main.go",
		},
		"operator-dashboard": {
			"docs/public/observability/dashboards/eshu-operator-overview.json",
		},
	}
	goalHookCases, err := filepath.Glob(filepath.Join(repoRoot, "scripts", "test-goal-*cases*.sh"))
	if err != nil {
		t.Fatalf("glob goal hook cases: %v", err)
	}
	appendRepoPaths(cases, "agent-canon", repoRoot, goalHookCases)
	parserFixtures, err := filepath.Glob(filepath.Join(repoRoot, "scripts", "lib", "test-verify-parser-relationship-kit-*"))
	if err != nil {
		t.Fatalf("glob parser relationship fixtures: %v", err)
	}
	appendRepoPaths(cases, "parser-relationship-kit", repoRoot, parserFixtures)

	for gateID, paths := range cases {
		gate := committedGate(t, registry, gateID)
		for _, path := range paths {
			path := path
			t.Run(gateID+"/"+strings.ReplaceAll(path, "/", "_"), func(t *testing.T) {
				t.Parallel()
				selected := registry.Select([]string{path}, cigates.TierPrePR)
				if !selectionForGate(t, selected, gateID).Selected {
					t.Fatalf("%s does not select gate %s", path, gateID)
				}
				if !gate.ShouldRunSelfTest([]string{path}) {
					t.Fatalf("%s selects %s but skips its distinct verifier self-test", path, gateID)
				}
			})
		}
	}
}

func appendRepoPaths(cases map[string][]string, gateID, repoRoot string, paths []string) {
	for _, path := range paths {
		cases[gateID] = append(cases[gateID], filepath.ToSlash(strings.TrimPrefix(path, repoRoot+string(filepath.Separator))))
	}
}

func committedGate(t *testing.T, registry *cigates.Registry, gateID string) cigates.Gate {
	t.Helper()
	for _, gate := range registry.Gates {
		if gate.ID == gateID {
			return gate
		}
	}
	t.Fatalf("committed registry has no gate %q", gateID)
	return cigates.Gate{}
}

func selectionForGate(t *testing.T, selections []cigates.Selection, gateID string) cigates.Selection {
	t.Helper()
	for _, selection := range selections {
		if selection.Gate.ID == gateID {
			return selection
		}
	}
	t.Fatalf("selection set has no gate %q", gateID)
	return cigates.Selection{}
}
