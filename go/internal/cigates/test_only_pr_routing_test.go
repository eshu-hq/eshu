// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadAllCallableFragments(t *testing.T, root string) []PRWorkflowDefinition {
	t.Helper()
	dir := filepath.Join(root, "specs", "ci-pr-workflows.d")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var defs []PRWorkflowDefinition
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".yml") && !strings.HasSuffix(entry.Name(), ".yaml")) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		parsed, err := ParsePRPlanFragment(raw)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		defs = append(defs, parsed...)
	}
	return defs
}

func TestTestOnlyPRsSkipProductionOnlyWorkflowFragments(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	defs := loadAllCallableFragments(t, root)

	// PR #11464: single Postgres live test under go/internal/query/supply/chain/impact/.
	pr11464 := []string{"go/internal/query/supply/chain/impact/multi_support_parity_live_test.go"}
	planned11464, err := PlanPRWorkflows(defs, pr11464, false, "main", "full", nil)
	if err != nil {
		t.Fatalf("PlanPRWorkflows(PR #11464): %v", err)
	}
	for _, skipped := range []string{
		"build.yml",
		"golden-corpus-gate.yml",
		"live-postgres-readiness.yml",
		"frontend.yml",
		"payload-usage-manifest.yml",
	} {
		if _, ok := planned11464[skipped]; ok {
			t.Errorf("PR #11464 test-only path unexpectedly selected %s", skipped)
		}
	}

	// PR #11465: three *_test.go files under go/internal/storage/postgres/.
	pr11465 := []string{
		"go/internal/storage/postgres/supply_chain_impact_winners_keyed_export_test.go",
		"go/internal/storage/postgres/supply_chain_impact_winners_keyed_fanout_live_test.go",
		"go/internal/storage/postgres/supply_chain_impact_winners_narrow_write_proof_live_test.go",
	}
	planned11465, err := PlanPRWorkflows(defs, pr11465, false, "main", "full", nil)
	if err != nil {
		t.Fatalf("PlanPRWorkflows(PR #11465): %v", err)
	}
	for _, skipped := range []string{
		"build.yml",
		"golden-corpus-gate.yml",
		"live-postgres-readiness.yml",
		"frontend.yml",
		"payload-usage-manifest.yml",
	} {
		if _, ok := planned11465[skipped]; ok {
			t.Errorf("PR #11465 test-only paths unexpectedly selected %s", skipped)
		}
	}

	// Production Go edit under go/internal/query/ must still select all production PR gates.
	prodQuery := []string{"go/internal/query/supply/chain/impact/store.go"}
	plannedProd, err := PlanPRWorkflows(defs, prodQuery, false, "main", "full", nil)
	if err != nil {
		t.Fatalf("PlanPRWorkflows(prod query): %v", err)
	}
	for _, want := range []string{
		"build.yml",
		"frontend.yml",
		"payload-usage-manifest.yml",
	} {
		if _, ok := plannedProd[want]; !ok {
			t.Errorf("production Go edit failed to select %s", want)
		}
	}

	// Re-included test files for payload-usage-manifest.yml and test-running
	// workflows (e2e-tests.yml, verify-replay-tier.yml) must stay selected.
	for _, tc := range []struct {
		name     string
		path     string
		workflow string
	}{
		{
			name:     "e2e conformance live test",
			path:     "go/internal/storage/cypher/nornic_conformance_test.go",
			workflow: "e2e-tests.yml",
		},
		{
			name:     "replay tier offlinetier test",
			path:     "go/internal/replay/offlinetier/postgres_test.go",
			workflow: "verify-replay-tier.yml",
		},
		{
			name:     "payload usage package test",
			path:     "go/internal/payloadusage/manifest_test.go",
			workflow: "payload-usage-manifest.yml",
		},
		{
			name:     "payload usage reducer drift test",
			path:     "go/internal/reducer/payload_usage_manifest_test.go",
			workflow: "payload-usage-manifest.yml",
		},
	} {
		got, err := PlanPRWorkflows(defs, []string{tc.path}, false, "main", "full", nil)
		if err != nil {
			t.Fatalf("PlanPRWorkflows(%s): %v", tc.name, err)
		}
		if _, ok := got[tc.workflow]; !ok {
			t.Errorf("%s (%s) must still select %s", tc.name, tc.path, tc.workflow)
		}
	}
}

func TestTestOnlyPRsSkipGoSecurityAndUnrelatedMCPFilters(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")

	secRaw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "security-scan.yml"))
	if err != nil {
		t.Fatal(err)
	}
	secSteps, err := DornyFilterStepsResolved(root, secRaw)
	if err != nil {
		t.Fatalf("resolve security-scan.yml filters: %v", err)
	}
	goSec, ok := DornyFilterStepWithKey(secSteps, "go_security")
	if !ok || goSec.Every {
		t.Fatal("security-scan.yml missing some-quantifier go_security filter")
	}
	secNonTest, ok := DornyFilterStepWithKey(secSteps, "non_test")
	if !ok || !secNonTest.Every {
		t.Fatal("security-scan.yml missing every-quantifier non_test filter")
	}
	gosecSelected := func(paths []string) bool {
		return DornySomeMatch(goSec.Filters["go_security"], paths) &&
			DornyEveryMatch(secNonTest.Filters["non_test"], paths)
	}
	if gosecSelected([]string{"go/internal/query/supply/chain/impact/multi_support_parity_live_test.go"}) {
		t.Error("security-scan go_gosec unexpectedly selected go/**/*_test.go-only PR")
	}
	if !DornySomeMatch(goSec.Filters["go_security"], []string{"go/internal/query/saml_verifier_go20266909_test.go"}) {
		t.Error("security-scan go_security must keep selecting govulncheck advisory regression _test.go")
	}
	if !gosecSelected([]string{"go/internal/query/supply/chain/impact/store.go"}) {
		t.Error("security-scan go_gosec failed to select production Go PR")
	}
	if !gosecSelected([]string{"go/internal/query/a_test.go", "go/internal/query/a.go"}) {
		t.Error("security-scan go_gosec failed to select mixed test+prod PR")
	}
	secText := string(secRaw)
	if !strings.Contains(secText, "go_gosec: ${{ steps.go_security.outputs.go_security == 'true' && steps.non_test.outputs.non_test == 'true' }}") {
		t.Error("security-scan.yml missing go_gosec combined output")
	}
	if !strings.Contains(secText, "needs.changes.outputs.go_gosec == 'true'") {
		t.Error("security-scan.yml gosec job must gate on go_gosec")
	}

	mcpRaw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "mcp-schema-drift.yml"))
	if err != nil {
		t.Fatal(err)
	}
	mcpSteps, err := DornyFilterStepsResolved(root, mcpRaw)
	if err != nil {
		t.Fatalf("resolve mcp-schema-drift.yml filters: %v", err)
	}
	goMCP, ok := DornyFilterStepWithKey(mcpSteps, "go_mcp")
	if !ok || goMCP.Every {
		t.Fatal("mcp-schema-drift.yml missing some-quantifier go_mcp filter")
	}
	mcpNonTest, ok := DornyFilterStepWithKey(mcpSteps, "non_test")
	if !ok || !mcpNonTest.Every {
		t.Fatal("mcp-schema-drift.yml missing every-quantifier non_test filter")
	}
	mcpTree, ok := DornyFilterStepWithKey(mcpSteps, "mcp_tree")
	if !ok || mcpTree.Every {
		t.Fatal("mcp-schema-drift.yml missing some-quantifier mcp_tree filter")
	}
	mcpSelected := func(paths []string) bool {
		return DornySomeMatch(goMCP.Filters["go_mcp"], paths) &&
			(DornyEveryMatch(mcpNonTest.Filters["non_test"], paths) ||
				DornySomeMatch(mcpTree.Filters["mcp_tree"], paths))
	}
	if mcpSelected([]string{"go/internal/query/supply/chain/impact/multi_support_parity_live_test.go"}) {
		t.Error("mcp-schema-drift go_mcp unexpectedly selected unrelated _test.go-only PR")
	}
	if !mcpSelected([]string{"go/internal/mcp/tools_test.go"}) {
		t.Error("mcp-schema-drift go_mcp must still select go/internal/mcp/*_test.go")
	}
	if !mcpSelected([]string{"go/internal/query/store.go"}) {
		t.Error("mcp-schema-drift go_mcp must still select production Go PR")
	}
	mcpText := string(mcpRaw)
	if !strings.Contains(mcpText, "go_mcp: ${{ steps.go_mcp.outputs.go_mcp == 'true' && (steps.non_test.outputs.non_test == 'true' || steps.mcp_tree.outputs.mcp_tree == 'true') }}") {
		t.Error("mcp-schema-drift.yml missing go_mcp combined output expression")
	}
	if strings.Count(mcpText, "needs.changes.outputs.go_mcp == 'true'") < 3 {
		t.Error("mcp-schema-drift.yml Go MCP jobs must gate on needs.changes.outputs.go_mcp == 'true'")
	}
}

func TestTestWorkflowWiresScopedGoRaceHelpers(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "test.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"go_race_scope: ${{ steps.go_race_scope.outputs.scope }}",
		"go_race_shards: ${{ steps.go_race_scope.outputs.shards }}",
		"go_race_packages: ${{ steps.go_race_scope.outputs.packages }}",
		"run: bash scripts/ci/test-resolve-go-race-scope.sh",
		"run: bash scripts/ci/resolve-go-race-scope.sh >> \"$GITHUB_OUTPUT\"",
		"shard: ${{ fromJSON(needs.changes.outputs.go_race_shards || '[1,2,3,4,5,6,7,8,9,10,11,12]') }}",
		"run: bash ../scripts/ci/run-go-race-shard.sh",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("test.yml missing scoped go-race wiring %q", want)
		}
	}
}
