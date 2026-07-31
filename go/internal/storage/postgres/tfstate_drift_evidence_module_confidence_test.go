// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// Unit and loader-integration tests for the per-address
// module-resolution-confidence signal (issue #5572,
// tfstate_drift_evidence_module_confidence.go). Split out of
// tfstate_drift_evidence_module_prefix_test.go and
// tfstate_drift_evidence_module_integration_test.go to keep all three files
// under the CLAUDE.md 500-line cap; same package, same fixtures and helpers
// (runBuildModulePrefixMapWithConfidence, pathDepth, intToA) as the sibling
// files.

// TestBuildModulePrefixMapRecordsRegistryHeuristicAsLowConfidence proves the
// issue #5572 confidence signal: when a `terraform-aws-modules/vpc/aws`
// source at call-site "." is classified external_registry, the SAME
// directory the source would have resolved to as a local relative path
// ("terraform-aws-modules/vpc/aws") is recorded in the confidence map with
// the external_registry reason -- because that directory might genuinely be
// a local module the repo's own top-level naming coincidentally shadows
// (the ADR's own documented false-positive shape), not a proven registry
// reference.
func TestBuildModulePrefixMapRecordsRegistryHeuristicAsLowConfidence(t *testing.T) {
	t.Parallel()

	out, confidence, rec := runBuildModulePrefixMapWithConfidence(t, []queueFakeRows{{
		rows: [][]any{{fixtureModuleCallsArray(
			fixtureModuleCallRow("vpc", "terraform-aws-modules/vpc/aws", "main.tf"),
		)}},
	}})
	if len(out) != 0 {
		t.Fatalf("prefix map = %v, want empty (registry source unresolvable)", out)
	}
	if got := rec.reasons(); len(got) != 1 || got[0] != unresolvedReasonExternalRegistry {
		t.Fatalf("recorded reasons = %v, want [%s]", got, unresolvedReasonExternalRegistry)
	}
	reason, ok := confidence["terraform-aws-modules/vpc/aws"]
	if !ok {
		t.Fatalf("confidence map missing key %q (have %v)", "terraform-aws-modules/vpc/aws", confidence)
	}
	if reason != unresolvedReasonExternalRegistry {
		t.Fatalf("confidence[%q] = %q, want %q", "terraform-aws-modules/vpc/aws", reason, unresolvedReasonExternalRegistry)
	}
}

// TestBuildModulePrefixMapRecordsGitSourceWithoutLowConfidenceCandidate is
// the negative control: a real external_git source has no local directory a
// resource could plausibly live under (the fetched module content is never
// part of this repo's own terraform_resources facts), so it must NOT gain a
// confidence-map entry the way the registry-shorthand heuristic does.
func TestBuildModulePrefixMapRecordsGitSourceWithoutLowConfidenceCandidate(t *testing.T) {
	t.Parallel()

	_, confidence, rec := runBuildModulePrefixMapWithConfidence(t, []queueFakeRows{{
		rows: [][]any{{fixtureModuleCallsArray(
			fixtureModuleCallRow("vpc", "git::https://github.com/example/vpc.git", "main.tf"),
		)}},
	}})
	if got := rec.reasons(); len(got) != 1 || got[0] != unresolvedReasonExternalGit {
		t.Fatalf("recorded reasons = %v, want [%s]", got, unresolvedReasonExternalGit)
	}
	if len(confidence) != 0 {
		t.Fatalf("confidence map = %v, want empty (external_git has no local directory candidate)", confidence)
	}
}

// TestBuildModulePrefixMapRecordsDepthExceededAsLowConfidence proves the
// issue #5572 confidence signal for the OTHER documented root-module-fallback
// cause: a resolved local module chain abandoned at maxModulePrefixDepth.
// Unlike the registry-shorthand heuristic, this callee directory is a real,
// already-resolved local path (walkModulePrefixChain's depth check fires
// before out[call.callee] is populated) -- so the confidence map must record
// it too, and the SAME directory must be genuinely absent from the real
// prefix map (proving this is a true gap, not just a telemetry counter).
func TestBuildModulePrefixMapRecordsDepthExceededAsLowConfidence(t *testing.T) {
	t.Parallel()

	chain := make([]string, 0, maxModulePrefixDepth+1)
	prevDir := "."
	for i := 0; i <= maxModulePrefixDepth; i++ {
		callerFile := "main.tf"
		if prevDir != "." {
			callerFile = prevDir + "/main.tf"
		}
		chain = append(chain, fixtureModuleCallRow(
			"m"+intToA(i),
			"./inner",
			callerFile,
		))
		if prevDir == "." {
			prevDir = "inner"
		} else {
			prevDir = prevDir + "/inner"
		}
	}
	// prevDir now holds the (maxModulePrefixDepth+1)-th callee directory --
	// the one walkModulePrefixChain abandons at the depth check, before ever
	// calling out[call.callee] = ... for it.
	deepestCallee := prevDir

	rows := make([][]any, len(chain))
	for i, row := range chain {
		rows[i] = []any{fixtureModuleCallsArray(row)}
	}
	out, confidence, rec := runBuildModulePrefixMapWithConfidence(t, []queueFakeRows{{rows: rows}})

	if got := rec.reasons(); !slices.Contains(got, unresolvedReasonDepthExceeded) {
		t.Fatalf("recorded reasons = %v, want at least one %s", got, unresolvedReasonDepthExceeded)
	}
	if _, ok := out[deepestCallee]; ok {
		t.Fatalf("prefix map unexpectedly contains the depth-exceeded callee %q (have %v) -- fixture no longer proves a real gap", deepestCallee, out)
	}
	reason, ok := confidence[deepestCallee]
	if !ok {
		t.Fatalf("confidence map missing depth-exceeded callee %q (have %v)", deepestCallee, confidence)
	}
	if reason != unresolvedReasonDepthExceeded {
		t.Fatalf("confidence[%q] = %q, want %q", deepestCallee, reason, unresolvedReasonDepthExceeded)
	}
}

// TestModuleResolutionReasonForEntryIgnoresShallowerConfidenceMatch proves
// the OTHER half of the depth comparison in moduleResolutionReasonForEntry
// (tfstate_drift_evidence_module_confidence.go, issue #5572): a confidence
// entry that is SHALLOWER than the directory that supplied the file's real
// prefix must be ignored. This covers a directory that independently hosts
// both an unrelated unresolved module call (e.g. a misclassified sibling
// `source` string at that directory) AND a genuinely resolved, deeper nested
// module -- the deeper, exact match must win; a problem at an ancestor
// directory must not cast doubt on a file whose own directory resolved
// cleanly.
func TestModuleResolutionReasonForEntryIgnoresShallowerConfidenceMatch(t *testing.T) {
	t.Parallel()

	confidence := moduleResolutionConfidenceMap{
		"modules/b": unresolvedReasonExternalRegistry,
	}
	// pathDepth("modules/b") == 2 (two segments); the file's own directory
	// "modules/b/child" is a deeper, EXACT match (depth 3) -- so the
	// shallower confidence entry on the ancestor "modules/b" must not apply.
	got := moduleResolutionReasonForEntry(confidence, "modules/b/child/main.tf", pathDepth("modules/b/child"))
	if got != "" {
		t.Fatalf("moduleResolutionReasonForEntry() = %q, want empty (deeper real match must win over a shallower confidence entry)", got)
	}
}

// TestModuleResolutionReasonForEntryAppliesEqualDepthConfidenceMatch is the
// boundary case: a confidence match at the SAME depth as the directory that
// supplied the prefix (a resolved-but-wrong ancestor exactly at the file's
// own matched directory) must still apply -- "at least as deep" per the
// function's doc comment, not "strictly deeper."
func TestModuleResolutionReasonForEntryAppliesEqualDepthConfidenceMatch(t *testing.T) {
	t.Parallel()

	confidence := moduleResolutionConfidenceMap{
		"modules/b": unresolvedReasonDepthExceeded,
	}
	// Both the confidence entry and the (hypothetical) prefix match land on
	// the SAME directory, "modules/b" (pathDepth 2) -- e.g. a resolved
	// ancestor's prefix came from this exact directory, which was ALSO
	// recorded low-confidence.
	got := moduleResolutionReasonForEntry(confidence, "modules/b/main.tf", pathDepth("modules/b"))
	if got != unresolvedReasonDepthExceeded {
		t.Fatalf("moduleResolutionReasonForEntry() = %q, want %q (equal-depth confidence match must still apply)", got, unresolvedReasonDepthExceeded)
	}
}

// TestLoadDriftEvidenceMarksLowConfidenceForRegistryHeuristicMisclassifiedLocalModule
// proves issue #5572's first documented cause end-to-end through
// LoadDriftEvidence: a repo whose module source string happens to match the
// Terraform-Registry-shorthand pattern ("terraform-aws-modules/vpc/aws") but
// is actually a real local directory containing real resources. The address
// this loader builds is the WRONG, unprefixed root-module shape (matching
// pre-#5572 v0 behavior — this fix does not change the address itself, only
// whether callers are told to distrust it) — but the config row must now be
// flagged ModuleResolutionReason == "external_registry" so BuildCandidates
// attaches the confidence atom and the reducer writer downgrades the
// finding's Outcome to "derived" instead of "exact".
func TestLoadDriftEvidenceMarksLowConfidenceForRegistryHeuristicMisclassifiedLocalModule(t *testing.T) {
	t.Parallel()

	anchor := tfstatebackend.CommitAnchor{
		RepoID: "repo-a", ScopeID: "repository:repo-a", CommitID: "gen-a1",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. terraform_modules: one call whose source LOOKS like registry
			//    shorthand but is actually the repo's own local directory.
			{rows: [][]any{{fixtureModuleCallsArray(
				fixtureModuleCallRow("vpc", "terraform-aws-modules/vpc/aws", "main.tf"),
			)}}},
			// 2. terraform_resources: a real resource physically declared
			//    under that same directory.
			{rows: [][]any{{fixtureConfigResourcesArray(
				fixtureConfigParserRowAtPath("aws_instance", "web", "terraform-aws-modules/vpc/aws/main.tf"),
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
	if got, want := rows[0].Address, "aws_instance.web"; got != want {
		t.Fatalf("Address = %q, want %q (root-module fallback -- the classic false positive)", got, want)
	}
	if rows[0].Config == nil {
		t.Fatal("rows[0].Config = nil, want a config-side row")
	}
	if got, want := rows[0].Config.ModuleResolutionReason, "external_registry"; got != want {
		t.Fatalf("ModuleResolutionReason = %q, want %q", got, want)
	}
}

// TestLoadDriftEvidenceMarksLowConfidenceForDepthExceededModuleChain proves
// issue #5572's second documented cause end-to-end: a resolved local module
// chain deeper than maxModulePrefixDepth. The deepest callee directory never
// gets a real prefix entry (walkModulePrefixChain abandons it before
// recording).
//
// This does NOT produce a plain root-module address the way the
// registry-heuristic case does: modulePrefixMap's ancestor-walk-up finds the
// immediately shallower directory's prefix instead (depth_exceeded requires
// depths 1..N-1 to have already resolved, so that ancestor is always
// populated) and silently misattributes the resource to the wrong, too-
// shallow parent module. That masked-but-wrong address is what this test
// asserts -- proving the depth-aware comparison in
// moduleResolutionReasonForEntry catches this case too, not just the
// "zero prefixes" shape. See tfstate_drift_evidence_module_confidence.go's
// package doc comment for the full mechanics; this test is what disproved
// the initial "zero prefixes" design and forced the depth-comparison fix.
func TestLoadDriftEvidenceMarksLowConfidenceForDepthExceededModuleChain(t *testing.T) {
	t.Parallel()

	chain := make([]string, 0, maxModulePrefixDepth+1)
	prevDir := "."
	for i := 0; i <= maxModulePrefixDepth; i++ {
		callerFile := "main.tf"
		if prevDir != "." {
			callerFile = prevDir + "/main.tf"
		}
		chain = append(chain, fixtureModuleCallRow(
			"m"+intToA(i),
			"./inner",
			callerFile,
		))
		if prevDir == "." {
			prevDir = "inner"
		} else {
			prevDir = prevDir + "/inner"
		}
	}
	deepestCallee := prevDir
	moduleRows := make([][]any, len(chain))
	for i, row := range chain {
		moduleRows[i] = []any{fixtureModuleCallsArray(row)}
	}

	anchor := tfstatebackend.CommitAnchor{
		RepoID: "repo-a", ScopeID: "repository:repo-a", CommitID: "gen-a1",
	}
	stateScopeID := "state_snapshot:s3:hash-xyz"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			// 1. terraform_modules: the depth-11 chain.
			{rows: moduleRows},
			// 2. terraform_resources: a resource declared at the abandoned,
			//    deepest callee directory.
			{rows: [][]any{{fixtureConfigResourcesArray(
				fixtureConfigParserRowAtPath("aws_instance", "web", deepestCallee+"/main.tf"),
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
	// The address is the MASKED, too-shallow parent prefix (module.m0
	// through module.m9 -- the ten successfully-resolved ancestor levels),
	// not a plain root-module address. This is the real bug: it looks like
	// a plausible, correctly-nested address, so nothing about its SHAPE
	// signals a problem.
	wantAddress := "module.m0.module.m1.module.m2.module.m3.module.m4." +
		"module.m5.module.m6.module.m7.module.m8.module.m9.aws_instance.web"
	if got := rows[0].Address; got != wantAddress {
		t.Fatalf("Address = %q, want %q (masked too-shallow parent prefix)", got, wantAddress)
	}
	if rows[0].Config == nil {
		t.Fatal("rows[0].Config = nil, want a config-side row")
	}
	if got, want := rows[0].Config.ModuleResolutionReason, "depth_exceeded"; got != want {
		t.Fatalf("ModuleResolutionReason = %q, want %q (the depth comparison must still flag this masked address low-confidence)", got, want)
	}
}
