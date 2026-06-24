// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"testing"
)

// TestIAMEscalationCatalogIsWellFormed locks the curated catalog against the most
// likely authoring mistakes: a non-lowercase action (which would never match the
// lowercased aws_iam_permission actions), an empty token or action, a duplicate
// primitive token, or a PassRole-family entry that does not require iam:passrole.
// The catalog is the security review focus, so a malformed entry is a correctness
// bug, not a style nit.
func TestIAMEscalationCatalogIsWellFormed(t *testing.T) {
	t.Parallel()

	seenTokens := make(map[string]struct{})
	for _, primitive := range iamEscalationCatalog {
		if strings.TrimSpace(primitive.Token) == "" {
			t.Fatalf("catalog entry has empty token: %+v", primitive)
		}
		if _, dup := seenTokens[primitive.Token]; dup {
			t.Fatalf("duplicate catalog token %q", primitive.Token)
		}
		seenTokens[primitive.Token] = struct{}{}

		if len(primitive.Actions) == 0 {
			t.Fatalf("catalog entry %q has no actions", primitive.Token)
		}
		for _, action := range primitive.Actions {
			if action != strings.ToLower(action) {
				t.Fatalf("catalog action %q for %q is not lowercase (aws_iam_permission lowercases actions)", action, primitive.Token)
			}
			if strings.TrimSpace(action) == "" {
				t.Fatalf("catalog entry %q has an empty action", primitive.Token)
			}
		}

		if primitive.TargetKind == iamEscalationTargetPassedRole {
			if primitive.PassRoleAction != iamEscalationPassRoleAction {
				t.Fatalf("passed-role primitive %q must name iam:passrole as its PassRoleAction, got %q", primitive.Token, primitive.PassRoleAction)
			}
			if !containsAny(primitive.Actions, iamEscalationPassRoleAction) {
				t.Fatalf("passed-role primitive %q must require iam:passrole in its actions", primitive.Token)
			}
		} else if primitive.PassRoleAction != "" {
			t.Fatalf("non-passed-role primitive %q must not set PassRoleAction", primitive.Token)
		}
	}
}

// TestIAMEscalationCatalogActionsIncludeStsAssumeRole proves the action-union set
// the Deny / catalog-touch logic keys on includes sts:AssumeRole, so a Deny on
// sts:AssumeRole is recognized and the deferred path stays observable.
func TestIAMEscalationCatalogActionsIncludeStsAssumeRole(t *testing.T) {
	t.Parallel()

	actions := iamEscalationCatalogActions()
	if _, ok := actions[iamEscalationStsAssumeRoleAction]; !ok {
		t.Fatal("catalog action union must include sts:assumerole")
	}
	if _, ok := actions["iam:createpolicyversion"]; !ok {
		t.Fatal("catalog action union must include the policy-mutation actions")
	}
}
