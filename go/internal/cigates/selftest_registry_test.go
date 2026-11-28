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
		"ci-gate-registry": {
			"AGENTS.md",
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
		"operator-dashboard": {
			"docs/public/observability/dashboards/eshu-operator-overview.json",
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
		"filename-stutter": {
			"scripts/verify-filename-stutter.sh",
			"scripts/test-verify-filename-stutter.sh",
		},
		"fips-md5-sql-sweep": {
			"go/internal/storage/postgres/md5_sql_sweep_test.go",
		},
		"no-ephemeral-docs": {
			"scripts/verify-no-ephemeral-docs.sh",
			"scripts/test-verify-no-ephemeral-docs.sh",
			"scripts/lib/gate-diff-base.sh",
		},
		"perf-evidence": {
			"scripts/verify-performance-evidence.sh",
			"scripts/test-verify-performance-evidence.sh",
			"scripts/lib/gate-diff-base.sh",
		},
		"payload-usage-manifest": {
			"go/internal/reducer/payload_usage_manifest_test.go",
			"scripts/verify-payload-usage-manifest.sh",
			"scripts/test-verify-payload-usage-manifest.sh",
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
		"docs-build-changed": {
			"specs/product-claims.v1.yaml",
			"AGENTS.md",
			"README.md",
		},
		"filename-stutter": {
			"go/internal/runtime/service.go",
		},
		"no-diff-fragments": {
			"go/internal/runtime/service.go",
		},
		"scope-prefix-single-source": {
			"go/internal/runtime/service.go",
		},
		"fips-md5-sql-sweep": {
			"go/internal/runtime/service.go",
		},
		"index-key-guard-sweep": {
			"go/internal/reducer/service.go",
		},
		"hot-cypher-source-coverage": {
			"go/internal/query/service.go",
		},
		"no-ephemeral-docs": {
			"docs/internal/evidence/note.md",
		},
		"perf-evidence": {
			"go/internal/reducer/service.go",
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
