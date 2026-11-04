// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factimpact

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func branchNamed(t *testing.T, name string) Branch {
	t.Helper()
	for _, branch := range Branches() {
		if branch.Name == name {
			return branch
		}
	}
	t.Fatalf("catalog has no branch %s", name)
	return Branch{}
}

// TestUpstreamBranchReadsOnlySignalKinds pins the #9914 fallback. Reading the cve,
// affected_package or affected_product kinds for an upstream id is the widening
// the shim failed, and affected_product is the term that can spend the budget.
func TestUpstreamBranchReadsOnlySignalKinds(t *testing.T) {
	t.Parallel()

	branch := branchNamed(t, BranchVulnerabilityUpstreamCVEID)
	if want := []string{KindEPSSScore, KindKnownExploited}; !reflect.DeepEqual(branch.Kinds, want) {
		t.Errorf("Kinds = %v, want %v", branch.Kinds, want)
	}
	if want := []Param{UpstreamCVEIDs}; !reflect.DeepEqual(branch.Params, want) {
		t.Errorf("Params = %v, want %v", branch.Params, want)
	}
	if branch.Anchor != IndexVulnerabilityCVEV2 || branch.ProbeKey != "cve_id" {
		t.Errorf("anchor/key = %s/%s, want the cve key probe", branch.Anchor, branch.ProbeKey)
	}
	if branch.MaxListLength != MaxCVEIDsPerStatement || MaxCVEIDsPerStatement != 120 {
		t.Errorf("MaxListLength = %d, const %d, want 120: the shared bound every CVE key probe uses",
			branch.MaxListLength, MaxCVEIDsPerStatement)
	}
	for _, kind := range []string{KindCVE, KindAffectedPackage, KindAffectedProduct} {
		if strings.Contains(branch.SQL, kind) {
			t.Errorf("statement names %s:\n%s", kind, branch.SQL)
		}
	}
}

// TestSuppressionStatementReadsTheUpstreamScope: a suppression scoped to a CVE a
// distro record names must load, however its scope spells the id, and the
// statement must run when the upstream list is the only input.
func TestSuppressionStatementReadsTheUpstreamScope(t *testing.T) {
	t.Parallel()

	branch := branchNamed(t, "vulnerability_suppression")
	if !slices.Contains(branch.Params, UpstreamCVEIDs) || !slices.Contains(branch.RunWhen, UpstreamCVEIDs) {
		t.Errorf("Params = %v, RunWhen = %v, want both to carry UpstreamCVEIDs", branch.Params, branch.RunWhen)
	}
	want := "upper(btrim(fact.payload->'scope'->>'cve_id', " + SuppressionScopeTrimCharactersSQL + "))"
	if !strings.Contains(branch.SQL, want) {
		t.Errorf("statement does not normalize the scope with %s:\n%s", want, branch.SQL)
	}
}

// TestUpstreamListReachesNothingElse: no other statement binds the list. The guard
// reads rows with a foreign key and is not a way to find a signal.
func TestUpstreamListReachesNothingElse(t *testing.T) {
	t.Parallel()

	for _, branch := range Branches() {
		if branch.Name == BranchVulnerabilityUpstreamCVEID || branch.Name == "vulnerability_suppression" {
			continue
		}
		if slices.Contains(branch.Params, UpstreamCVEIDs) {
			t.Errorf("%s binds UpstreamCVEIDs", branch.Name)
		}
	}
}
