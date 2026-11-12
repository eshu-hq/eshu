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
// skip-scans every scope. Mirrors factKeyPlanViolations. #10525 adds the
// fact_work_items generation index (migration 294) to the condition map
// only, not to retentionGuarded10512Tables, so small fixtures that
// legitimately seq-scan the near-empty work table in other statements
// stay unguarded by this half. The #10525 statements additionally run
// retentionWorkItemsPlanViolations10525, which accounts every
// fact_work_items access: the row-count statement accesses the table
// twice, and the required-index check below alone would let one bound
// arm cover a scanning other arm.
var retentionGuarded10512IndexConditions = map[string][]string{
	"shared_projection_intents_generation_idx":        {"generation_id ="},
	"fact_work_items_generation_idx":                  {"generation_id ="},
	"package_manifest_consumption_keys_partition_idx": {"scope_id =", "generation_id ="},
	"package_registry_identity_keys_partition_idx":    {"scope_id =", "generation_id ="},
}

// retentionPlanRoot10512 decodes an EXPLAIN (FORMAT JSON) plan and
// returns its root node.
func retentionPlanRoot10512(t *testing.T, plan []byte) map[string]any {
	t.Helper()
	var root []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(plan, &root); err != nil || len(root) != 1 {
		t.Fatalf("decode plan: %v", err)
	}
	return root[0].Plan
}

// retentionGuardedPlanViolations walks an EXPLAIN (FORMAT JSON) plan and
// reports each sequential scan of a guarded table, each probe of a guarded
// index whose Index Cond does not bind every column in
// retentionGuarded10512IndexConditions (a cond-less or partly bound probe
// grows with the table, not the batch), and each required index the plan
// never probes.
func retentionGuardedPlanViolations(t *testing.T, plan []byte, required []string) []string {
	t.Helper()
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
	walk(retentionPlanRoot10512(t, plan))
	for _, index := range required {
		if !used[index] {
			violations = append(violations, "never probes "+index)
		}
	}
	return violations
}

// retentionWorkItemsBoundProbe10525 reports whether an index probe of
// fact_work_items is bound on the batch (#10525): the migration 294
// generation index bound on generation_id, or the scope-leading
// scope_generation index bound on both columns. A generation-only probe
// of the scope-leading index skip-scans every scope, and a scope-only
// probe reads every generation of a scope, so neither is bound.
func retentionWorkItemsBoundProbe10525(index, condition string) bool {
	switch index {
	case "fact_work_items_generation_idx":
		return strings.Contains(condition, "generation_id =")
	case "fact_work_items_scope_generation_idx":
		return strings.Contains(condition, "scope_id =") && strings.Contains(condition, "generation_id =")
	default:
		return false
	}
}

// retentionWorkItemsPlanViolations10525 walks an EXPLAIN (FORMAT JSON)
// plan and reports every access of fact_work_items that is not a bound
// generation probe (#10525). It applies only to the #10525 statements,
// whose every fact_work_items access probes by generation: the row-count
// statement accesses the table twice (the generation-only count arm and
// the scope-joined deployable_unit_reopen_fingerprints arm), so the
// required-index half of retentionGuardedPlanViolations, which one bound
// probe satisfies, cannot see a sequential scan or a partly bound
// scope-leading probe in the other arm. A Bitmap Heap Scan names no
// index itself; its Bitmap Index Scan children are the probes and are
// checked as the walk descends (a bitmap index node may carry no
// Relation Name, so it is also recognized by its index name).
func retentionWorkItemsPlanViolations10525(t *testing.T, plan []byte) []string {
	t.Helper()
	var violations []string
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		nodeType, _ := node["Node Type"].(string)
		relation, _ := node["Relation Name"].(string)
		index, _ := node["Index Name"].(string)
		condition, _ := node["Index Cond"].(string)
		access := relation == "fact_work_items" ||
			(nodeType == "Bitmap Index Scan" && strings.HasPrefix(index, "fact_work_items"))
		if access {
			switch {
			case nodeType == "Bitmap Heap Scan":
				// Reported by its Bitmap Index Scan children below.
			case nodeType == "Seq Scan":
				violations = append(violations, "Seq Scan on fact_work_items")
			case !retentionWorkItemsBoundProbe10525(index, condition):
				violations = append(violations, fmt.Sprintf("%s on fact_work_items via %s is not a bound generation probe (%s)", nodeType, index, condition))
			}
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			if childNode, ok := child.(map[string]any); ok {
				walk(childNode)
			}
		}
	}
	walk(retentionPlanRoot10512(t, plan))
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

// retentionPlanLeaf10525 is one synthetic leaf scan node of a multi-access
// plan fixture (#10525).
type retentionPlanLeaf10525 struct {
	nodeType  string
	relation  string
	index     string
	condition string
}

// retentionGuardedPlanFixture10525 builds a synthetic EXPLAIN (FORMAT JSON)
// plan whose Append parent holds every leaf, so one statement can carry
// several accesses of one relation, as generationRetentionRowCountsQuery
// carries two fact_work_items accesses (#10525).
func retentionGuardedPlanFixture10525(leaves ...retentionPlanLeaf10525) []byte {
	children := make([]string, len(leaves))
	for i, leaf := range leaves {
		cond := ""
		if leaf.condition != "" {
			cond = fmt.Sprintf(`, "Index Cond": %q`, leaf.condition)
		}
		children[i] = fmt.Sprintf(`{"Node Type": %q, "Relation Name": %q, "Index Name": %q%s}`,
			leaf.nodeType, leaf.relation, leaf.index, cond)
	}
	return []byte(fmt.Sprintf(`[{"Plan": {"Node Type": "Append", "Plans": [%s]}}]`, strings.Join(children, ", ")))
}

// retentionBitmapWorkItemsPlanFixture10525 builds a synthetic plan whose
// second fact_work_items access is a Bitmap Heap Scan over one Bitmap
// Index Scan child, next to a bound migration 294 probe (#10525). The
// child carries no Relation Name, as in a real bitmap plan, so the guard
// must recognize it by its index name.
func retentionBitmapWorkItemsPlanFixture10525(index, condition string) []byte {
	return []byte(fmt.Sprintf(`[{"Plan": {"Node Type": "Append", "Plans": [`+
		`{"Node Type": "Index Only Scan", "Relation Name": "fact_work_items", "Index Name": "fact_work_items_generation_idx", "Index Cond": "(generation_id = candidate.generation_id)"}, `+
		`{"Node Type": "Bitmap Heap Scan", "Relation Name": "fact_work_items", "Plans": [{"Node Type": "Bitmap Index Scan", "Index Name": %q, "Index Cond": %q}]}]}}]`,
		index, condition))
}

// TestRetentionWorkItemsPlanViolationsRejectMixedPlans10525 is the seeded
// RED pair for the per-access half of the #10525 guard: the row-count
// statement accesses fact_work_items twice, so a bound probe of the
// migration 294 index in one arm must not cover a sequential scan or a
// partly bound scope-leading probe in the other arm.
func TestRetentionWorkItemsPlanViolationsRejectMixedPlans10525(t *testing.T) {
	t.Parallel()
	bound294 := retentionPlanLeaf10525{"Index Only Scan", "fact_work_items", "fact_work_items_generation_idx", "(generation_id = candidate.generation_id)"}
	for _, tc := range []struct {
		name      string
		plan      []byte
		wantClean bool
	}{
		{
			name: "bound 294 probe with sequential second access",
			plan: retentionGuardedPlanFixture10525(bound294,
				retentionPlanLeaf10525{"Seq Scan", "fact_work_items", "", ""}),
		},
		{
			name: "bound 294 probe with generation-only scope-leading second access",
			plan: retentionGuardedPlanFixture10525(bound294,
				retentionPlanLeaf10525{"Index Scan", "fact_work_items", "fact_work_items_scope_generation_idx", "(generation_id = candidate.generation_id)"}),
		},
		{
			name: "bound 294 probe with scope-only scope-leading second access",
			plan: retentionGuardedPlanFixture10525(bound294,
				retentionPlanLeaf10525{"Index Scan", "fact_work_items", "fact_work_items_scope_generation_idx", "(scope_id = generation.scope_id)"}),
		},
		{
			name: "bound 294 probe with fully bound scope-leading second access",
			plan: retentionGuardedPlanFixture10525(bound294,
				retentionPlanLeaf10525{"Index Scan", "fact_work_items", "fact_work_items_scope_generation_idx", "((scope_id = generation.scope_id) AND (generation_id = candidate.generation_id))"}),
			wantClean: true,
		},
		{
			name:      "two bound 294 probes",
			plan:      retentionGuardedPlanFixture10525(bound294, bound294),
			wantClean: true,
		},
		{
			name: "bound 294 probe with cond-less 294 second access",
			plan: retentionGuardedPlanFixture10525(bound294,
				retentionPlanLeaf10525{"Index Only Scan", "fact_work_items", "fact_work_items_generation_idx", ""}),
		},
		{
			name: "bound 294 probe with primary-key second access",
			plan: retentionGuardedPlanFixture10525(bound294,
				retentionPlanLeaf10525{"Index Scan", "fact_work_items", "fact_work_items_pkey", "(work_item_id = fp.work_item_id)"}),
		},
		{
			name: "bound 294 probe with bitmap generation-only scope-leading second access",
			plan: retentionBitmapWorkItemsPlanFixture10525("fact_work_items_scope_generation_idx", "(generation_id = candidate.generation_id)"),
		},
		{
			name:      "bound 294 probe with bitmap bound 294 second access",
			plan:      retentionBitmapWorkItemsPlanFixture10525("fact_work_items_generation_idx", "(generation_id = candidate.generation_id)"),
			wantClean: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			violations := retentionWorkItemsPlanViolations10525(t, tc.plan)
			if tc.wantClean && len(violations) > 0 {
				t.Fatalf("guard rejected all-bound accesses: %s", strings.Join(violations, "; "))
			}
			if !tc.wantClean && len(violations) == 0 {
				t.Fatalf("guard accepted a mixed plan with an unbound fact_work_items access:\n%s", tc.plan)
			}
		})
	}
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
		{
			// #10525: same seeded pair for the work-items generation index.
			name:     "cond-less work items index-only scan",
			plan:     retentionGuardedPlanFixture10512("Index Only Scan", "fact_work_items", "fact_work_items_generation_idx", ""),
			required: []string{"fact_work_items_generation_idx"},
		},
		{
			name:      "bound work items probe",
			plan:      retentionGuardedPlanFixture10512("Index Only Scan", "fact_work_items", "fact_work_items_generation_idx", "(generation_id = candidate.generation_id)"),
			required:  []string{"fact_work_items_generation_idx"},
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
