// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// retentionGuarded10512Tables are the tables whose retention access must
// stay an index probe whose cost follows the batch (#10512): the intents
// table behind migration 292 and the two package key sidecars behind
// migrations 182 and 183.
var retentionGuarded10512Tables = map[string]string{
	"shared_projection_intents":         "shared_projection_intents_generation_idx",
	"package_manifest_consumption_keys": "package_manifest_consumption_keys_partition_idx",
	"package_registry_identity_keys":    "package_registry_identity_keys_partition_idx",
}

// retentionGuarded10512IndexConditions is the Index Cond column bindings a
// probe of each guarded index must carry (#10512): the intents index leads
// with generation_id alone, so that binding is the whole probe; the two
// package key partition indexes lead with (scope_id, generation_id), and a
// probe bound on only one of them reads every generation of a scope or
// skip-scans every scope. Mirrors factKeyPlanViolations.
var retentionGuarded10512IndexConditions = map[string][]string{
	"shared_projection_intents_generation_idx":        {"generation_id ="},
	"package_manifest_consumption_keys_partition_idx": {"scope_id =", "generation_id ="},
	"package_registry_identity_keys_partition_idx":    {"scope_id =", "generation_id ="},
}

// retentionGuardedPlanViolations walks an EXPLAIN (FORMAT JSON) plan and
// reports each sequential scan of a guarded table, each probe of a guarded
// index whose Index Cond does not bind every column in
// retentionGuarded10512IndexConditions (a cond-less or partly bound probe
// grows with the table, not the batch), and each required index the plan
// never probes.
func retentionGuardedPlanViolations(t *testing.T, plan []byte, required []string) []string {
	t.Helper()
	var root []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(plan, &root); err != nil || len(root) != 1 {
		t.Fatalf("decode plan: %v", err)
	}
	var violations []string
	used := map[string]bool{}
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		nodeType, _ := node["Node Type"].(string)
		relation, _ := node["Relation Name"].(string)
		index, _ := node["Index Name"].(string)
		condition, _ := node["Index Cond"].(string)
		if index != "" {
			used[index] = true
		}
		if _, guarded := retentionGuarded10512Tables[relation]; guarded && nodeType == "Seq Scan" {
			violations = append(violations, "Seq Scan on "+relation)
		}
		if bindings, guarded := retentionGuarded10512IndexConditions[index]; guarded {
			var missing []string
			for _, binding := range bindings {
				if !strings.Contains(condition, binding) {
					missing = append(missing, strings.TrimSuffix(binding, " ="))
				}
			}
			if len(missing) > 0 {
				violations = append(violations, fmt.Sprintf("%s on %s without %s (%s)", nodeType, index, strings.Join(missing, " and "), condition))
			}
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			if childNode, ok := child.(map[string]any); ok {
				walk(childNode)
			}
		}
	}
	walk(root[0].Plan)
	for _, index := range required {
		if !used[index] {
			violations = append(violations, "never probes "+index)
		}
	}
	return violations
}

// retentionGuardedPlanFixture10512 is one synthetic EXPLAIN (FORMAT JSON)
// node, so the guard's Index Cond sensitivity is proven without depending on
// which plan a given Postgres chooses for a broken production predicate.
func retentionGuardedPlanFixture10512(nodeType, relation, index, condition string) []byte {
	cond := ""
	if condition != "" {
		cond = fmt.Sprintf(`, "Index Cond": %q`, condition)
	}
	return []byte(fmt.Sprintf(`[{"Plan": {"Node Type": %q, "Relation Name": %q, "Index Name": %q%s}}]`,
		nodeType, relation, index, cond))
}

// TestRetentionGuardedPlanViolationsRejectUnboundProbes10512 is the seeded
// RED pair for the Index Cond half of the guard (#10512): a full index-only
// scan of the intents generation index and a scope-only probe of a partition
// index both name the required index and seq-scan nothing, so only the
// condition check can reject them; fully bound probes of both shapes pass.
func TestRetentionGuardedPlanViolationsRejectUnboundProbes10512(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		plan      []byte
		required  []string
		wantClean bool
	}{
		{
			name:     "cond-less intents index-only scan",
			plan:     retentionGuardedPlanFixture10512("Index Only Scan", "shared_projection_intents", "shared_projection_intents_generation_idx", ""),
			required: []string{"shared_projection_intents_generation_idx"},
		},
		{
			name:     "scope-only partition probe",
			plan:     retentionGuardedPlanFixture10512("Index Scan", "package_manifest_consumption_keys", "package_manifest_consumption_keys_partition_idx", "(scope_id = generation.scope_id)"),
			required: []string{"package_manifest_consumption_keys_partition_idx"},
		},
		{
			name:     "generation-only partition probe",
			plan:     retentionGuardedPlanFixture10512("Bitmap Index Scan", "package_registry_identity_keys", "package_registry_identity_keys_partition_idx", "(generation_id = candidate.generation_id)"),
			required: []string{"package_registry_identity_keys_partition_idx"},
		},
		{
			name:      "bound intents probe",
			plan:      retentionGuardedPlanFixture10512("Index Only Scan", "shared_projection_intents", "shared_projection_intents_generation_idx", "(generation_id = ANY ('{g1,g2}'::text[]))"),
			required:  []string{"shared_projection_intents_generation_idx"},
			wantClean: true,
		},
		{
			name:      "bound partition probe",
			plan:      retentionGuardedPlanFixture10512("Index Only Scan", "package_manifest_consumption_keys", "package_manifest_consumption_keys_partition_idx", "((scope_id = generation.scope_id) AND (generation_id = candidate.generation_id))"),
			required:  []string{"package_manifest_consumption_keys_partition_idx"},
			wantClean: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			violations := retentionGuardedPlanViolations(t, tc.plan, tc.required)
			if tc.wantClean && len(violations) > 0 {
				t.Fatalf("guard rejected a bound probe: %s", strings.Join(violations, "; "))
			}
			if !tc.wantClean && len(violations) == 0 {
				t.Fatalf("guard accepted an unbound probe:\n%s", tc.plan)
			}
		})
	}
}
