package postgres

import (
	"context"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// Integration tests for module-aware drift joining (issue #169 / ADR
// 2026-05-11-module-aware-drift-joining). Split from the unit-test file at
// tfstate_drift_evidence_module_prefix_test.go to keep both files under the
// CLAUDE.md 500-line cap. Test fixtures and recorder helpers live in the
// other file; tests here exercise the full PostgresDriftEvidenceLoader path
// through LoadDriftEvidence.

func TestLoadConfigByAddressAppliesModulePrefixForCalleeFiles(t *testing.T) {
	t.Parallel()

	anchor := tfstatebackend.CommitAnchor{
		RepoID:      "repo-a",
		ScopeID:     "repository:repo-a",
		CommitID:    "gen-a1",
		BackendKind: "s3",
		LocatorHash: "hash-xyz",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. terraform_modules: one local-source call.
			{rows: [][]any{{fixtureModuleCallsArray(
				fixtureModuleCallRow("vpc", "./modules/vpc", "main.tf"),
			)}}},
			// 2. terraform_resources: one resource inside the callee.
			{rows: [][]any{{fixtureConfigResourcesArray(
				fixtureConfigParserRowAtPath("aws_instance", "web", "modules/vpc/main.tf"),
			)}}},
			// 3. snapshot serial=0.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 0, "gen-state-current")}},
			// 4. state-resource: nothing.
			{rows: [][]any{}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db}

	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("LoadDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := rows[0].Address, "module.vpc.aws_instance.web"; got != want {
		t.Fatalf("Address = %q, want %q", got, want)
	}
}

func TestLoadConfigByAddressExpandsSameCalleeForMultipleCallers(t *testing.T) {
	t.Parallel()

	// BINDING CONSTRAINT D — load-bearing 1→N projection test. One
	// terraform_resources fact in modules/vpc/main.tf is referenced by TWO
	// distinct module {} blocks (vpc_a and vpc_b). The loader must emit
	// TWO ResourceRow values with distinct prefixed addresses; the fan-out
	// lives in the loader emission loop, NOT in configRowFromParserEntry.
	anchor := tfstatebackend.CommitAnchor{
		RepoID:      "repo-a",
		ScopeID:     "repository:repo-a",
		CommitID:    "gen-a1",
		BackendKind: "s3",
		LocatorHash: "hash-xyz",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. terraform_modules: TWO calls, same callee.
			{rows: [][]any{{fixtureModuleCallsArray(
				fixtureModuleCallRow("vpc_a", "./modules/vpc", "main.tf"),
				fixtureModuleCallRow("vpc_b", "./modules/vpc", "main.tf"),
			)}}},
			// 2. terraform_resources: ONE entry under the shared callee.
			{rows: [][]any{{fixtureConfigResourcesArray(
				fixtureConfigParserRowAtPath("aws_instance", "web", "modules/vpc/main.tf"),
			)}}},
			// 3. snapshot serial=0.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 0, "gen-state-current")}},
			// 4. state-resource: nothing.
			{rows: [][]any{}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db}

	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("LoadDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d (1→N projection failed — fan-out missing)", got, want)
	}
	addresses := []string{rows[0].Address, rows[1].Address}
	sort.Strings(addresses)
	want := []string{"module.vpc_a.aws_instance.web", "module.vpc_b.aws_instance.web"}
	if !sliceEqual(addresses, want) {
		t.Fatalf("addresses = %v, want %v", addresses, want)
	}
}

func TestLoadConfigByAddressNestedChainProducesMultiLevelAddress(t *testing.T) {
	t.Parallel()

	anchor := tfstatebackend.CommitAnchor{
		RepoID: "repo-a", ScopeID: "repository:repo-a", CommitID: "gen-a1",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. Two-level chain: root calls platform; platform calls vpc.
			{rows: [][]any{
				{fixtureModuleCallsArray(fixtureModuleCallRow("platform", "./modules/platform", "main.tf"))},
				{fixtureModuleCallsArray(fixtureModuleCallRow("vpc", "./vpc", "modules/platform/main.tf"))},
			}},
			// 2. Resource inside the inner callee.
			{rows: [][]any{{fixtureConfigResourcesArray(
				fixtureConfigParserRowAtPath("aws_instance", "web", "modules/platform/vpc/main.tf"),
			)}}},
			// 3-4. snapshot empty path.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 0, "gen-state-current")}},
			{rows: [][]any{}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db}

	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("LoadDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := rows[0].Address, "module.platform.module.vpc.aws_instance.web"; got != want {
		t.Fatalf("Address = %q, want %q", got, want)
	}
}

func TestLoadConfigByAddressRootModuleResourcesKeepIdenticalAddress(t *testing.T) {
	t.Parallel()

	// Regression baseline: a repo with zero `module {}` blocks must
	// produce byte-identical `<type>.<name>` addresses. Failing this means
	// the change leaked into the common case.
	anchor := tfstatebackend.CommitAnchor{
		RepoID: "repo-a", ScopeID: "repository:repo-a", CommitID: "gen-a1",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. terraform_modules: empty.
			{rows: [][]any{}},
			// 2. resource at repo root.
			{rows: [][]any{{fixtureConfigResourcesArray(
				fixtureConfigParserRowAtPath("aws_iam_role", "svc", "main.tf"),
			)}}},
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 0, "gen-state-current")}},
			{rows: [][]any{}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db}
	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("LoadDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := rows[0].Address, "aws_iam_role.svc"; got != want {
		t.Fatalf("Address = %q, want %q (root-module byte-identical regression)", got, want)
	}
}

func TestLoadConfigByAddressMissingTerraformModulesFactDegradesGracefully(t *testing.T) {
	t.Parallel()

	// Loader sees zero terraform_modules facts (parser bug or repo with no
	// modules). Every nested resource falls back to root-module address.
	anchor := tfstatebackend.CommitAnchor{
		RepoID: "repo-a", ScopeID: "repository:repo-a", CommitID: "gen-a1",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// terraform_modules: empty (the bug case).
			{rows: [][]any{}},
			// resource that "should" be prefixed if we had module facts.
			{rows: [][]any{{fixtureConfigResourcesArray(
				fixtureConfigParserRowAtPath("aws_instance", "web", "modules/vpc/main.tf"),
			)}}},
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 0, "gen-state-current")}},
			{rows: [][]any{}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db}
	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("LoadDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := rows[0].Address, "aws_instance.web"; got != want {
		t.Fatalf("Address = %q, want %q (no module facts → root-module fallback)", got, want)
	}
}

// TestLoadPriorConfigAddressesAppliesModulePrefix validates the PR #191
// same-PR requirement (binding ADR Q5): a module-nested resource block
// deleted in the current generation while the surrounding module call still
// exists must trigger removed_from_config — which only works if the
// prior-config walk applies a generation-appropriate module-prefix map.
func TestLoadPriorConfigAddressesAppliesModulePrefix(t *testing.T) {
	t.Parallel()

	anchor := tfstatebackend.CommitAnchor{
		RepoID: "repo-a", ScopeID: "repository:repo-a", CommitID: "gen-a2",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. terraform_modules CURRENT gen: still has the module call.
			{rows: [][]any{{fixtureModuleCallsArray(
				fixtureModuleCallRow("vpc", "./modules/vpc", "main.tf"),
			)}}},
			// 2. config-side CURRENT gen: empty (resource was deleted).
			{rows: [][]any{}},
			// 3. snapshot serial=5.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 5, "gen-state-current")}},
			// 4. current state-resource: still has the prefixed address.
			{rows: [][]any{fixtureStateResourceRow(
				"module.vpc.aws_instance.web",
				fixtureStatePayload("module.vpc.aws_instance.web", "aws_instance", "web", `{}`),
			)}},
			// 5. prior snapshot.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 4, "gen-state-prior")}},
			// 6. prior state-resource (same address).
			{rows: [][]any{fixtureStateResourceRow(
				"module.vpc.aws_instance.web",
				fixtureStatePayload("module.vpc.aws_instance.web", "aws_instance", "web", `{}`),
			)}},
			// 7. prior-config walk: prior gen had the resource at the callee path.
			{rows: [][]any{{"gen-a1", fixturePriorConfigAddressesArray(
				fixtureConfigParserRowAtPath("aws_instance", "web", "modules/vpc/main.tf"),
			)}}},
			// 8. terraform_modules PRIOR gen: the surrounding module call still
			//    existed, so the prior-config address set contains
			//    "module.vpc.aws_instance.web" and matches the state row.
			{rows: [][]any{{fixtureModuleCallsArray(
				fixtureModuleCallRow("vpc", "./modules/vpc", "main.tf"),
			)}}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db, PriorConfigDepth: 10}
	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("LoadDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if rows[0].State == nil {
		t.Fatalf("row.State = nil, want non-nil")
	}
	if !rows[0].State.PreviouslyDeclaredInConfig {
		t.Fatalf("row.State.PreviouslyDeclaredInConfig = false, want true (prior-config walk must apply module-prefix map)")
	}
}

func TestLoadPriorConfigAddressesUsesPriorGenerationModulePrefixOnRename(t *testing.T) {
	t.Parallel()

	// Regression for issue #201. The current repo generation renamed the
	// module block from "vpc" to "network" while the same callee path still
	// contains aws_instance.web. State still uses Terraform's prior canonical
	// address, module.vpc.aws_instance.web. The prior-config walk must project
	// the prior resource with the prior generation's module name; applying the
	// current generation's "network" prefix would leave the state-only row
	// indistinguishable from an operator import.
	anchor := tfstatebackend.CommitAnchor{
		RepoID: "repo-a", ScopeID: "repository:repo-a", CommitID: "gen-a2",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. terraform_modules CURRENT gen: module was renamed.
			{rows: [][]any{{fixtureModuleCallsArray(
				fixtureModuleCallRow("network", "./modules/vpc", "main.tf"),
			)}}},
			// 2. config-side CURRENT gen: same callee resource now projects to
			//    module.network.aws_instance.web.
			{rows: [][]any{{fixtureConfigResourcesArray(
				fixtureConfigParserRowAtPath("aws_instance", "web", "modules/vpc/main.tf"),
			)}}},
			// 3. snapshot serial=5.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 5, "gen-state-current")}},
			// 4. current state-resource: Terraform state still carries the old
			//    module address.
			{rows: [][]any{fixtureStateResourceRow(
				"module.vpc.aws_instance.web",
				fixtureStatePayload("module.vpc.aws_instance.web", "aws_instance", "web", `{}`),
			)}},
			// 5. prior snapshot.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 4, "gen-state-prior")}},
			// 6. prior state-resource: same old address.
			{rows: [][]any{fixtureStateResourceRow(
				"module.vpc.aws_instance.web",
				fixtureStatePayload("module.vpc.aws_instance.web", "aws_instance", "web", `{}`),
			)}},
			// 7. prior-config walk: prior gen had the same callee resource, but
			//    it must be interpreted through prior module name "vpc", not the
			//    current module name "network".
			{rows: [][]any{{"gen-a1", fixturePriorConfigAddressesArray(
				fixtureConfigParserRowAtPath("aws_instance", "web", "modules/vpc/main.tf"),
			)}}},
			// 8. terraform_modules PRIOR gen: old module name.
			{rows: [][]any{{fixtureModuleCallsArray(
				fixtureModuleCallRow("vpc", "./modules/vpc", "main.tf"),
			)}}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db, PriorConfigDepth: 10}

	rows, err := loader.LoadDriftEvidence(context.Background(), stateScopeID, anchor)
	if err != nil {
		t.Fatalf("LoadDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	byAddress := map[string]bool{}
	for _, row := range rows {
		if row.State != nil {
			byAddress[row.Address] = row.State.PreviouslyDeclaredInConfig
		}
	}
	if !byAddress["module.vpc.aws_instance.web"] {
		t.Fatalf("module.vpc state row was not marked previously declared; prior-generation module prefix was not used")
	}
}
