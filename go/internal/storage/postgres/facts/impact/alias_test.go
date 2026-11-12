// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factimpact

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// suppressionPlaceholder renders the $n::text[] placeholder the suppression
// branch binds one list at, resolved through the branch's own bind order.
func suppressionPlaceholder(branch Branch, param Param) string {
	for i, bound := range branch.Params {
		if bound == param {
			return "$" + strconv.Itoa(i+1) + "::text[]"
		}
	}
	return ""
}

// TestSuppressionStatementReadsTheAliasMembers is the #10408 L3 failing
// regression: a suppression naming a dead (merged-away) id must load during
// the re-key pass. Alias members discovered in-pass (AliasIDs) have to pull a
// dead-id suppression the same way the CVE/advisory arms pull live-id ones,
// or the evaluator change behind findingMatchableID never sees it and
// the merged finding stays visible. The statement must run when the alias
// list is the only input.
func TestSuppressionStatementReadsTheAliasMembers(t *testing.T) {
	t.Parallel()

	branch := branchNamed(t, "vulnerability_suppression")
	if !slices.Contains(branch.Params, AliasIDs) || !slices.Contains(branch.RunWhen, AliasIDs) {
		t.Errorf("Params = %v, RunWhen = %v, want both to carry AliasIDs", branch.Params, branch.RunWhen)
	}
	aliasRef := suppressionPlaceholder(branch, AliasIDs)
	for _, key := range []string{"cve_id", "advisory_id"} {
		want := "fact.payload->>'" + key + "' = ANY(" + aliasRef + ")"
		if !strings.Contains(branch.SQL, want) {
			t.Errorf("suppression statement does not match dead ids on %q with %q:\n%s", key, want, branch.SQL)
		}
	}
	if !slices.Contains(branch.Params, LowerAliasIDs) {
		t.Errorf("suppression Params = %v, want it to carry LowerAliasIDs for the normalized scope arms", branch.Params)
	}
	lowerRef := suppressionPlaceholder(branch, LowerAliasIDs)
	for _, key := range []string{"cve_id", "advisory_id"} {
		want := "lower(btrim(fact.payload->'scope'->>'" + key + "', " + SuppressionScopeTrimCharactersSQL + ")) = ANY(" + lowerRef + ")"
		if !strings.Contains(branch.SQL, want) {
			t.Errorf("suppression statement does not match dead scope ids on %q with %q:\n%s", key, want, branch.SQL)
		}
	}
}

// TestAliasListReachesOnlyTheSuppressionStatement pins the LowerAliasIDs
// blast radius: the alias probes bind AliasIDs for vulnerability.cve rows,
// but the lowered twin serves only the suppression scope arms. Any other
// statement binding it would widen a second kind by dead ids.
func TestAliasListReachesOnlyTheSuppressionStatement(t *testing.T) {
	t.Parallel()

	for _, branch := range Branches() {
		if branch.Name == "vulnerability_suppression" {
			continue
		}
		if slices.Contains(branch.Params, LowerAliasIDs) {
			t.Errorf("%s binds LowerAliasIDs", branch.Name)
		}
	}
}
