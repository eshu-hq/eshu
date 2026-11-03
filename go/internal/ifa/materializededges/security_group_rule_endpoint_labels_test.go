// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckEndpointTargetLabelsBindsLabelPerRule proves the hermetic guard
// pins the per-edge label binding (#6228 review P2): the shared fixture
// compares edge identity alone, so a regression resolving the right uid
// under the wrong label must fail here, not pass through to the live tier.
func TestCheckEndpointTargetLabelsBindsLabelPerRule(t *testing.T) {
	t.Parallel()

	matching := []map[string]any{}
	for ruleUID, label := range securityGroupRuleEndpointTargetLabelByRule {
		matching = append(matching, map[string]any{
			"rule_uid":     ruleUID,
			"target_label": label,
		})
	}
	if got := checkSecurityGroupRuleEndpointTargetLabels("probe", matching); got != "" {
		t.Fatalf("matching labels = %q, want clean", got)
	}

	swapped := []map[string]any{{
		"rule_uid":     "0a3afef6f922fb2707a353db2d131190432d0120fe17988f3b5d0a69768c8161",
		"target_label": "PrefixList",
	}}
	if got := checkSecurityGroupRuleEndpointTargetLabels("probe", swapped); !strings.Contains(got, "want \"CidrBlock\"") {
		t.Fatalf("swapped label = %q, want a want-CidrBlock failure", got)
	}

	unknown := []map[string]any{{
		"rule_uid":     "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		"target_label": "CidrBlock",
	}}
	if got := checkSecurityGroupRuleEndpointTargetLabels("probe", unknown); !strings.Contains(got, "unlisted rule_uid") {
		t.Fatalf("unknown rule = %q, want a fail-closed unlisted-rule failure", got)
	}
}

// TestEndpointGuardInvokesTargetLabelCheck pins the wiring the cases above
// cannot reach: the helper is only real if the guard calls it. A swapped
// label with a known rule_uid is unreachable through the production
// extractor (the uid folds the same source tokens the label derives from),
// so no Odu-level input distinguishes "call present" from "call deleted" —
// deleting the call-site below must fail this test instead.
//
// The pin parses the guard into an AST and requires the check as a branch
// condition: a raw substring check would also match the call commented out,
// and a bare call expression would match a discarded result, so neither
// pins enforcement. Only `if problem := check…; problem != ""` satisfies it.
func TestEndpointGuardInvokesTargetLabelCheck(t *testing.T) {
	t.Parallel()

	path := filepath.Join(repoRootDir(t), "go", "internal", "ifa", "materializededges", "security_group_rule_endpoint.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse guard source: %v", err)
	}
	enforced := false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			branch, ok := node.(*ast.IfStmt)
			if !ok {
				return true
			}
			// The call may sit in the init (`if problem := check…; …`) or
			// the condition; either way the branch enforces its result.
			for _, root := range []ast.Node{branch.Init, branch.Cond} {
				if root == nil {
					continue
				}
				ast.Inspect(root, func(inner ast.Node) bool {
					call, ok := inner.(*ast.CallExpr)
					if !ok {
						return true
					}
					if name, ok := call.Fun.(*ast.Ident); ok && name.Name == "checkSecurityGroupRuleEndpointTargetLabels" {
						enforced = true
						return false
					}
					return true
				})
			}
			return true
		})
	}
	if !enforced {
		t.Fatalf("guard no longer enforces checkSecurityGroupRuleEndpointTargetLabels as a branch condition; the label regression cases above pass while swapped-label rows flow unchecked")
	}
}
