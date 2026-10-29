// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"slices"
	"strings"
)

// docsSetPatterns is the CI docs set: changed paths that cannot affect
// compiled, tested, or scanned behavior. It mirrors the dorny/paths-filter
// `code` negations in test.yml, security-scan.yml, and mcp-schema-drift.yml
// and the build.yml plan fragment paths_ignore, translated into MatchGlob
// dialect (where "**" already crosses segments, so "**/*.md" covers the root
// "*.md" case too):
//
//   - docs/** — the whole docs tree (prose, dashboards, evidence, fixtures)
//   - **/*.md — markdown at any depth
//   - *.json — root-level JSON (package.json, tsconfigs, .prettierrc)
//   - docs/**/*.json, scripts/**/*.json — scoped config and inventory JSON
//   - mkdocs.yml, .github/**/*.md, .agents/** — nav, workflow notes, skills
//
// Deliberately NOT in the set: specs/** JSON (image pins feed the query-plan
// image contract), go/**, sdk/**, tests/**, and testdata/** JSON (unit tests
// and go:embed directives consume them), and every other code tree. A path
// outside the set matches triggers exactly as before.
var docsSetPatterns = []string{
	"docs/**",
	"**/*.md",
	"*.json",
	"docs/**/*.json",
	"scripts/**/*.json",
	"mkdocs.yml",
	".github/**/*.md",
	".agents/**",
}

// lockstepDocsPaths are docs-set-shaped files whose content is an asserted
// Go test input (family tables, exported surfaces, doc fences, runbooks,
// evidence JSON). Editing one can break a unit test, so they stay code in
// every selection layer. The canonical list lives in
// specs/ci-docs-lockstep-paths.txt; TestLockstepPathsMatchManifest pins this
// copy to it, and TestDocsTestInputsStayCode discovers new test inputs.
var lockstepDocsPaths = []string{
	"AGENTS.md",
	"docs/internal/design/430-nornicdb-graph-search-split.md",
	"docs/internal/design/5351-ifa-materialized-edge-known-gaps.md",
	"docs/internal/evidence/8869-collector-vault-live-census.md",
	"docs/internal/evidence/8870-vulnerability-intelligence-version-provenance.md",
	"docs/internal/evidence/8871-eshu-deploy-census.md",
	"docs/internal/evidence/8871-eshu-deploy-contract.md",
	"docs/internal/evidence/8874-eshu-cli-contract.md",
	"docs/internal/remote-validation/inventory.generated.json",
	"docs/public/deploy/kubernetes/helm-routing-and-storage-values.md",
	"docs/public/deploy/kubernetes/runtime-parity-matrix.md",
	"docs/public/deployment/credential-file-cleanup.md",
	"docs/public/deployment/service-runtimes-collectors.md",
	"docs/public/deployment/service-runtimes.md",
	"docs/public/languages/admitted-files.md",
	"docs/public/operate/3am-operator-runbook.md",
	"docs/public/reference/assistant-fast-path-hooks.md",
	"docs/public/reference/env-registry.md",
	"docs/public/reference/environment-collectors-supply-chain.md",
	"docs/public/reference/environment-collectors.md",
	"docs/public/reference/environment-ingestion-queues.md",
	"docs/public/reference/first-run-evidence.md",
	"docs/public/reference/generated/replay-coverage.md",
	"docs/public/reference/graph-backend-installation.md",
	"docs/public/reference/http-api.md",
	"docs/public/reference/http-api/collector-terminal-failure.md",
	"docs/public/reference/hybrid-retrieval-production-gate.md",
	"docs/public/reference/local-performance-envelope.md",
	"docs/public/reference/mcp-cookbook.md",
	"docs/public/reference/mcp-prompt-surface-audit.md",
	"docs/public/reference/mcp-tool-contract-matrix.md",
	"docs/public/reference/nornicdb-tuning.md",
	"docs/public/reference/reducer-claim-latency-gate.md",
	"docs/public/reference/searchbench-evidence/issue-1298-semantic-retrieval-proof-v1.json",
	"docs/public/reference/searchbench-evidence/issue-417-nornicdb-hybrid-retrieval-prototype-v1.json",
	"docs/public/reference/searchbench-evidence/issue-420-link-prediction-evaluation-v1.json",
	"docs/public/reference/searchbench-evidence/issue-421-rerank-protocol-evaluation-v1.json",
	"docs/public/run-locally/docker-compose.md",
	"go/cmd/README.md",
	"go/cmd/api/CUTOVER_OWNERSHIP.md",
	"go/cmd/audit-preflight/testdata/codegraphcontext-already-tracked.md",
	"go/cmd/audit-preflight/testdata/gitnexus-missing.md",
	"go/cmd/audit-preflight/testdata/graphify-foundation-exists.md",
	"go/cmd/audit-preflight/testdata/incomplete.md",
	"go/cmd/collector-vault-live/testdata/8869-collector-vault-live-census.md",
	"go/cmd/eshu/cli_extraction_inventory.md",
	"go/cmd/ifa/README.md",
	"go/cmd/mcp-server/CUTOVER_OWNERSHIP.md",
	"go/cmd/scanner-worker/EXTRACTION_PREP.md",
	"go/internal/ask/catalog/README.md",
	"go/internal/cli/demo/README.md",
	"go/internal/cli/hookpreflight/AGENTS.md",
	"go/internal/cli/hookpreflight/README.md",
	"go/internal/collector/README.md",
	"go/internal/collector/sbom/SHIM_MAP.md",
	"go/internal/content/artifacts/README.md",
	"go/internal/facts/FACT_KIND_REGISTRIES.md",
	"go/internal/ifa/README.md",
	"go/internal/ifa/materializededges/README.md",
	"go/internal/ifa/testdata/5998-rationale-relative-path.md",
	"go/internal/query/repository/README.md",
	"go/internal/reducer/AGENTS.md",
	"go/internal/reducer/README.md",
	"go/internal/reducer/testdata/5998-rationale-relative-path.md",
	"go/internal/runtime/README.md",
	"go/internal/runtime/testdata/8894-component-extension-activations.md",
	"testdata/golden/documentation-positive.md",
}

// IsDocsSetPath reports whether path is in the CI docs set: prose and scoped
// config that cannot affect compiled, tested, or scanned behavior. Lockstep
// test inputs stay code even when they look like prose.
func IsDocsSetPath(path string) bool {
	if slices.Contains(lockstepDocsPaths, path) {
		return false
	}
	for _, pattern := range docsSetPatterns {
		if MatchGlob(pattern, path) {
			return true
		}
	}
	return false
}

// triggerSelectsDocsPath reports whether trigger may select a docs-set path.
// The only triggers that never select for docs-set paths are whole-tree
// catch-alls over a tree that also holds code ("go/**"-style): their author
// meant "any code under here" and markdown rides along incidentally. Every
// other shape that matches is a deliberate family scope — a literal, a
// fixed-depth pattern, a catch-all over a docs-confined tree, a "**" with a
// docs-bound suffix, or the universal "**" — so the match itself is the
// selection signal. Content gates such as skill-roundtrip stay required for
// their own inputs. The gate still selects normally for any non-docs path.
func triggerSelectsDocsPath(trigger string) bool {
	if trigger == "**" {
		return true
	}
	if !strings.ContainsAny(trigger, "*?[") {
		return IsDocsSetPath(trigger)
	}
	if strings.HasSuffix(trigger, "/**") {
		return isDocsConfinedTree(strings.TrimSuffix(trigger, "/**"))
	}
	segments := strings.Split(trigger, "/")
	lastStar := -1
	for i, seg := range segments {
		if seg == "**" {
			lastStar = i
		}
	}
	if lastStar == -1 {
		return true
	}
	suffix := segments[lastStar+1:]
	if len(suffix) == 1 && strings.HasSuffix(suffix[0], ".md") {
		return true
	}
	if len(suffix) == 1 && suffix[0] == "*.json" {
		head := segments[0]
		return head == "docs" || head == "scripts"
	}
	return true
}

// isDocsConfinedTree reports whether root names a tree verified to hold only
// docs-set files. The dorny filter and MatchGlob agree on these shapes;
// single-star .md/.json trigger shapes have no live instances (checked
// 2026-10-26), and the guard's superset check pins the code-filter side.
//
// go/internal/parser is the one code-tree exception, and it is trigger-text
// scoped: parser-relationship-kit names exactly "go/internal/parser/**" and
// its dorny filter matches parser READMEs (rare-docs-reader contract), with
// golden-corpus-gate planned for the same paths, so selecting keeps the
// aggregate awaitable. Sibling code trees stay excluded: their gates back
// compiled or scanned behavior, not prose.
func isDocsConfinedTree(root string) bool {
	for _, tree := range []string{"docs", ".agents", "skill-fragments", "expected"} {
		if root == tree || strings.HasPrefix(root, tree+"/") {
			return true
		}
	}
	return root == "go/internal/parser" || strings.HasPrefix(root, "go/internal/parser/")
}

// codeGatedCIJobs are the test.yml job IDs whose pull_request `if` requires
// needs.changes.outputs.code == 'true', so they never run on a docs-only PR.
// TestCodeGatedJobsMatchWorkflowIfs pins this set against the workflow text:
// adding a code-gated lane there without listing it here strands the
// aggregate on docs-only PRs (selected but never produced).
var codeGatedCIJobs = map[string]struct{}{
	"merge-group-slim": {},
	"verify-contracts": {},
	"go-core":          {},
	"go-race":          {},
}

// allDocsSetChanged reports whether every changed path is in the CI docs set.
// An empty path list is not docs-only: AllBlockingGates (truncated giant PRs)
// must keep failing closed on the full set.
func allDocsSetChanged(paths []string) bool {
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if !IsDocsSetPath(path) {
			return false
		}
	}
	return true
}

// docsSetSkipsGate reports whether gate must not select when every changed
// path is docs-set: its CI job never runs on such a PR, so selecting would
// strand required-gates-complete on a check that is never produced. Mixed
// docs+code PRs are unaffected, merge_group uses the fixed slim set rather
// than path selection, and non-test.yml workflows keep their own gating.
func docsSetSkipsGate(gate Gate, changed []string) bool {
	if !allDocsSetChanged(changed) {
		return false
	}
	if gate.CI.Workflow != "test.yml" {
		return false
	}
	_, gated := codeGatedCIJobs[gate.CI.Job]
	return gated
}

// triggerMatchesChangedPath reports whether trigger selects path: plain glob
// matching, except a docs-set path is only selected by a trigger that can
// match docs-set paths alone. Selection for non-docs paths is unchanged, so a
// PR mixing docs with code still runs the full set.
func triggerMatchesChangedPath(trigger, path string) bool {
	if !MatchGlob(trigger, path) {
		return false
	}
	if !IsDocsSetPath(path) {
		return true
	}
	return triggerSelectsDocsPath(trigger)
}
