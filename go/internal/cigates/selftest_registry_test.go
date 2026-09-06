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
			"go/internal/reducer/crossrepo/cross_repo_evidence_type.go",
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
		"doc-citations": {
			"scripts/verify-doc-citations.sh",
			"scripts/test-verify-doc-citations.sh",
			"scripts/lib/doc-citation-lines.sh",
			"scripts/lib/test-verify-doc-citations-line-cases.sh",
			"scripts/lib/test-verify-doc-citations-review-cases.sh",
			"scripts/lib/test-verify-doc-citations-binary-cases.sh",
			"scripts/lib/test-verify-doc-citations-preparation-cases.sh",
			"scripts/lib/test-verify-doc-citations-scope-cases.sh",
			"scripts/lib/gate-diff-base.sh",
			"scripts/docs-citations-baseline.txt",
			"specs/ci-gates.v1.yaml",
			".github/workflows/static-contract-gates.yml",
		},
		"measurement-citations": {
			"scripts/verify-measurement-citations.sh",
			"scripts/test-verify-measurement-citations.sh",
		},
		"docs-build-changed": {
			"docs/public/index.md",
			"scripts/verify-docs-build-changed.sh",
			"scripts/test-verify-docs-build-changed.sh",
			"scripts/lib/test-verify-docs-build-changed-fake-uv.sh",
			"scripts/lib/context-stories-markdown-visible.awk",
			"scripts/test-context-stories-doc-split.sh",
			"scripts/test-context-stories-markdown-visible.sh",
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
		if gate.SelfTestTriggers == nil {
			t.Fatalf("gate %s does not declare self_test_triggers", gateID)
		}
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

func TestCommittedRegistryProductInputsSkipDistinctSelfTests(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	registry, err := cigates.Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("load committed registry: %v", err)
	}

	cases := map[string][]string{
		"doc-citations": {
			"docs/internal/agent-guide.md",
			"go/internal/cigates/select.go",
			"tests/fixtures/ecosystems/supply-chain-demo-db/statefulset.yaml",
		},
		"measurement-citations": {
			"docs/internal/evidence/example.md",
			"docs/public/index.md",
			"go/internal/reducer/evidence-example.md",
		},
		"docs-build-changed": {
			"specs/product-claims.v1.yaml",
			"AGENTS.md",
			"README.md",
		},
	}

	for gateID, paths := range cases {
		gateID, paths := gateID, paths
		gate := committedGate(t, registry, gateID)
		for _, path := range paths {
			path := path
			t.Run(gateID+"/"+strings.ReplaceAll(path, "/", "_"), func(t *testing.T) {
				t.Parallel()
				selected := registry.Select([]string{path}, cigates.TierPrePR)
				if !selectionForGate(t, selected, gateID).Selected {
					t.Fatalf("%s does not select primary gate %s", path, gateID)
				}
				if gate.ShouldRunSelfTest([]string{path}) {
					t.Fatalf("%s selects %s and unexpectedly runs its distinct verifier self-test", path, gateID)
				}
			})
		}
	}
}

func TestCommittedRegistryDocCitationPartitionsRepositoryProofFromFixtures(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	registry, err := cigates.Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("load committed registry: %v", err)
	}

	gate := committedGate(t, registry, "doc-citations")
	if !strings.Contains(gate.Local.Command, "--repository-only") {
		t.Fatalf("doc-citations primary command = %q, want repository-only proof", gate.Local.Command)
	}
	if !strings.Contains(gate.Local.TestCommand, "--fixtures-only") {
		t.Fatalf("doc-citations test command = %q, want fixtures-only proof", gate.Local.TestCommand)
	}
}

func TestCommittedRegistryRealTreeSelfTestsRemainFailClosed(t *testing.T) {
	t.Parallel()
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	registry, err := cigates.Load(filepath.Join(repoRoot, "specs", "ci-gates.v1.yaml"))
	if err != nil {
		t.Fatalf("load committed registry: %v", err)
	}

	for _, gateID := range []string{"docs-refs", "docs-cli-env-refs"} {
		gate := committedGate(t, registry, gateID)
		if gate.SelfTestTriggers != nil {
			t.Errorf("gate %s self_test_triggers = %v, want omitted fail-closed behavior for its real-tree baseline proof", gateID, gate.SelfTestTriggers)
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
