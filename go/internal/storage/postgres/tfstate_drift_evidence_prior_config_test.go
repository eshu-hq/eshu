// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// fixturePriorConfigAddressesArray wraps one or more parser rows into the JSON
// array shape returned by listPriorConfigAddressesQuery. The query SELECTs the
// terraform_resources array per file, so each row in the result is one such
// array. This helper mirrors fixtureConfigResourcesArray for the prior-gen
// shape.
func fixturePriorConfigAddressesArray(rows ...string) []byte {
	return fixtureConfigResourcesArray(rows...)
}

func TestPostgresDriftEvidenceLoaderPriorConfigDeclarationActivatesRemovedFromConfig(t *testing.T) {
	t.Parallel()

	anchor := tfstatebackend.CommitAnchor{
		RepoID:      "repo-a",
		ScopeID:     "repository:repo-a",
		CommitID:    "gen-a2",
		BackendKind: "s3",
		LocatorHash: "hash-xyz",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. terraform_modules walk (#169): no module calls in this fixture.
			{rows: [][]any{}},
			// 2. config-side (current gen): empty.
			{rows: [][]any{}},
			// 3. current snapshot: serial=5.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 5, "gen-state-current")}},
			// 4. current state-resource: an address that was once declared.
			{rows: [][]any{fixtureStateResourceRow(
				"aws_iam_policy.legacy",
				fixtureStatePayload("aws_iam_policy.legacy", "aws_iam_policy", "legacy", `{}`),
			)}},
			// 5. prior state snapshot lookup: same lineage, serial=4.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 4, "gen-state-prior")}},
			// 6. prior state-resource: same address.
			{rows: [][]any{fixtureStateResourceRow(
				"aws_iam_policy.legacy",
				fixtureStatePayload("aws_iam_policy.legacy", "aws_iam_policy", "legacy", `{}`),
			)}},
			// 7. prior-config addresses walk. The prior gen DID declare this address.
			{rows: [][]any{{
				"gen-a1",
				fixturePriorConfigAddressesArray(fixtureConfigParserRow("aws_iam_policy", "legacy")),
			}}},
			// 8. prior-generation terraform_modules walk: no module calls in this fixture.
			{rows: [][]any{}},
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
		t.Fatalf("row.State.PreviouslyDeclaredInConfig = false, want true (prior gen declared this address)")
	}
}

func TestPostgresDriftEvidenceLoaderPriorConfigNeverDeclaredLeavesFlagFalse(t *testing.T) {
	t.Parallel()

	anchor := tfstatebackend.CommitAnchor{
		RepoID:      "repo-a",
		ScopeID:     "repository:repo-a",
		CommitID:    "gen-a2",
		BackendKind: "s3",
		LocatorHash: "hash-xyz",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. terraform_modules walk (#169): no module calls in this fixture.
			{rows: [][]any{}},
			// 2. config-side: empty.
			{rows: [][]any{}},
			// 3. current snapshot: serial=5.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 5, "gen-state-current")}},
			// 4. current state-resource: an operator-imported address.
			{rows: [][]any{fixtureStateResourceRow(
				"aws_iam_role.imported",
				fixtureStatePayload("aws_iam_role.imported", "aws_iam_role", "imported", `{}`),
			)}},
			// 5. prior snapshot: same lineage.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 4, "gen-state-prior")}},
			// 6. prior state-resource: same address persisted.
			{rows: [][]any{fixtureStateResourceRow(
				"aws_iam_role.imported",
				fixtureStatePayload("aws_iam_role.imported", "aws_iam_role", "imported", `{}`),
			)}},
			// 7. prior-config walk: returns NO rows (address never declared anywhere).
			{rows: [][]any{}},
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
	if rows[0].State.PreviouslyDeclaredInConfig {
		t.Fatalf("row.State.PreviouslyDeclaredInConfig = true, want false (no prior gen declared this — operator-imported resource)")
	}
}

func TestPostgresDriftEvidenceLoaderPriorConfigOutsideDepthWindowLeavesFlagFalse(t *testing.T) {
	t.Parallel()

	// The address was declared N+1 generations ago. With PriorConfigDepth=1
	// the walker only sees the most recent prior generation, which does NOT
	// declare the address. PreviouslyDeclaredInConfig stays false; the
	// classifier emits added_in_state — the conservative outside-window
	// fallback documented in the spec.
	anchor := tfstatebackend.CommitAnchor{
		RepoID:      "repo-a",
		ScopeID:     "repository:repo-a",
		CommitID:    "gen-a2",
		BackendKind: "s3",
		LocatorHash: "hash-xyz",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// terraform_modules walk (#169): no module calls in this fixture.
			{rows: [][]any{}},
			{rows: [][]any{}},
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 5, "gen-state-current")}},
			{rows: [][]any{fixtureStateResourceRow(
				"aws_iam_policy.deep_history",
				fixtureStatePayload("aws_iam_policy.deep_history", "aws_iam_policy", "deep_history", `{}`),
			)}},
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 4, "gen-state-prior")}},
			{rows: [][]any{fixtureStateResourceRow(
				"aws_iam_policy.deep_history",
				fixtureStatePayload("aws_iam_policy.deep_history", "aws_iam_policy", "deep_history", `{}`),
			)}},
			// prior-config walk with depth=1: most recent prior gen has no entry for this address.
			{rows: [][]any{{
				"gen-a1",
				fixturePriorConfigAddressesArray(fixtureConfigParserRow("aws_iam_role", "unrelated")),
			}}},
			// prior-generation terraform_modules walk: no module calls in this fixture.
			{rows: [][]any{}},
		},
	}
	loader := PostgresDriftEvidenceLoader{DB: db, PriorConfigDepth: 1}

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
	if rows[0].State.PreviouslyDeclaredInConfig {
		t.Fatalf("row.State.PreviouslyDeclaredInConfig = true, want false (address only in N+1 deeper, outside depth window)")
	}
}

// TestPostgresDriftEvidenceLoaderPriorConfigConfidenceThreadedOntoRemovedFromConfigRow
// is the regression for issue #5572's second defect: buildModulePrefixMap's
// confidenceMap return was discarded (`priorPrefixMap, _, err :=
// l.buildModulePrefixMap(...)`) inside loadPriorConfigAddresses, and
// collectPriorConfigAddresses hardcoded "" for moduleResolutionReason. So a
// prior generation whose module resolution failed (the same
// external_registry / depth_exceeded heuristic fallback that flags
// config-side rows low-confidence in the current generation) silently
// produced a CERTAIN-looking prior-config address, even though that address
// was exactly as uncertain as its current-generation counterpart. When that
// heuristic address matched a real current-generation state address,
// mergeDriftRows promoted PreviouslyDeclaredInConfig=true, the classifier
// emitted removed_from_config, and the resulting candidate had no config row
// to inherit ModuleResolutionReason from — so the writer persisted
// outcome="exact" for a finding that was really "derived".
//
// This fixture mirrors the documented "repo whose top-level directory is
// literally terraform-aws-modules" false-positive
// (TestLoadDriftEvidenceMarksLowConfidenceForRegistryHeuristicMisclassifiedLocalModule)
// but at the PRIOR generation: the prior gen's module call source looks like
// registry shorthand but is actually local, so the prior-config address for
// aws_instance.web is a genuine root-module address that is nonetheless
// low-confidence. The current generation's real state resource lives at that
// same root address (matching the documented scenario where the resource
// truly is root-module, but the loader cannot tell that confidently).
func TestPostgresDriftEvidenceLoaderPriorConfigConfidenceThreadedOntoRemovedFromConfigRow(t *testing.T) {
	t.Parallel()

	anchor := tfstatebackend.CommitAnchor{
		RepoID:      "repo-a",
		ScopeID:     "repository:repo-a",
		CommitID:    "gen-a2",
		BackendKind: "s3",
		LocatorHash: "hash-xyz",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. terraform_modules CURRENT gen: empty.
			{rows: [][]any{}},
			// 2. config-side CURRENT gen: empty (resource deleted from config).
			{rows: [][]any{}},
			// 3. current snapshot: serial=5.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 5, "gen-state-current")}},
			// 4. current state-resource: real resource at the root address.
			{rows: [][]any{fixtureStateResourceRow(
				"aws_instance.web",
				fixtureStatePayload("aws_instance.web", "aws_instance", "web", `{}`),
			)}},
			// 5. prior state snapshot: same lineage, serial=4.
			{rows: [][]any{fixtureSnapshotRow("lineage-1", 4, "gen-state-prior")}},
			// 6. prior state-resource: same address.
			{rows: [][]any{fixtureStateResourceRow(
				"aws_instance.web",
				fixtureStatePayload("aws_instance.web", "aws_instance", "web", `{}`),
			)}},
			// 7. prior-config addresses walk: one generation, one entry
			//    physically declared under a directory that LOOKS like
			//    registry shorthand.
			{rows: [][]any{{
				"gen-a1",
				fixturePriorConfigAddressesArray(fixtureConfigParserRowAtPath(
					"aws_instance", "web", "terraform-aws-modules/vpc/aws/main.tf",
				)),
			}}},
			// 8. terraform_modules PRIOR gen: the call whose source
			//    misclassifies as registry shorthand.
			{rows: [][]any{{fixtureModuleCallsArray(
				fixtureModuleCallRow("vpc", "terraform-aws-modules/vpc/aws", "main.tf"),
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
		t.Fatalf("row.State.PreviouslyDeclaredInConfig = false, want true (prior gen declared this address)")
	}
	if got, want := rows[0].State.ModuleResolutionReason, "external_registry"; got != want {
		t.Fatalf("row.State.ModuleResolutionReason = %q, want %q (prior-config confidence must thread onto the removed_from_config candidate)", got, want)
	}
}

// Row-ordering regression tests for loadPriorConfigAddresses
// (TestListPriorConfigAddressesQueryOrdersByIngestedAtDescending,
// TestCollectPriorConfigAddressesFirstWriteWinsDependsOnCallOrder,
// TestPostgresDriftEvidenceLoaderPrefersMostRecentPriorGenerationConfidenceOnConflict)
// live in tfstate_drift_evidence_prior_config_ordering_test.go, split out to
// stay under the CLAUDE.md 500-line cap.

func TestRecordModuleRenameIfPrefixChangedRecordsOncePerPath(t *testing.T) {
	t.Parallel()

	recorder := &fakeUnresolvedRecorder{}
	seen := map[string]struct{}{}
	entry := map[string]any{"path": "modules/vpc/main.tf"}
	currentPrefixMap := modulePrefixMap{"modules/vpc": []string{"module.network"}}
	priorPrefixMap := modulePrefixMap{"modules/vpc": []string{"module.vpc"}}

	recordModuleRenameIfPrefixChanged(
		context.Background(),
		recorder,
		"gen-a1",
		entry,
		currentPrefixMap,
		priorPrefixMap,
		seen,
	)
	recordModuleRenameIfPrefixChanged(
		context.Background(),
		recorder,
		"gen-a1",
		entry,
		currentPrefixMap,
		priorPrefixMap,
		seen,
	)

	reasons := recorder.reasons()
	if got, want := len(reasons), 1; got != want {
		t.Fatalf("recorded reason count = %d, want %d: %v", got, want, reasons)
	}
	if got, want := reasons[0], unresolvedReasonModuleRenamed; got != want {
		t.Fatalf("recorded reason = %q, want %q", got, want)
	}
}
