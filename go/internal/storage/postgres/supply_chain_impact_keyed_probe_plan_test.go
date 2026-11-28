// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"strings"
	"testing"
)

// keyedProbeIndexName is the draft keyed probe index (migration 354).
const keyedProbeIndexName = "supply_chain_impact_finding_supports_keyed_probe_idx"

// keyedProbeEqualities are the identifier equalities the keyed probe applies
// as index conditions, as the planner prints them in an Index Cond (the
// operator is ->>, which contains the text "->"). Under the ruled #11691
// leniency the planner serves the probe as a BitmapOr over the 354 index:
// the package_id equality arm and its IS NULL arm are the index conditions,
// with the c0/advisory equalities common to both arms. The repository_id and
// subject_digest OR arms stay applied as a Filter (see
// keyedProbeFilterEqualities): the OR forces the planner to factor the
// bitmap on the leading leg, so index descent is via BitmapOr, not one
// five-column Index Cond.
var keyedProbeEqualities = map[string]string{
	"package_id":      "(payload ->> 'package_id'::text) = ident.package_id",
	"package_id_null": "(payload ->> 'package_id'::text) IS NULL",
	"cve_or_advisory": "NULLIF((payload ->> 'cve_id'::text), ''::text), NULLIF((payload ->> 'advisory_id'::text), ''::text), ''::text) = ident.c0",
	"advisory_id":     "COALESCE((payload ->> 'advisory_id'::text), ''::text) = ident.advisory_id",
}

// keyedProbeFilterEqualities are the leniency OR arms the planner applies as
// a Filter over the bitmap: still applied to every row, just not as index
// conditions. An arm that vanished entirely (neither index nor filter)
// widens or narrows the probe silently, so both sets are pinned.
var keyedProbeFilterEqualities = map[string]string{
	"repository_id":  "(payload ->> 'repository_id'::text) = ident.repository_id",
	"subject_digest": "(payload ->> 'subject_digest'::text) = ident.subject_digest",
}

// keyedProbePlanProblems lists what is wrong with a keyed probe plan: no use of the named index, a Seq Scan on
// the supports table, an index equality missing from every index node's
// Index Cond (BitmapOr arms are separate nodes), or a filter equality
// missing from the whole plan. Callers pass keyedProbeEqualities for the
// index set; the lab live test additionally checks
// keyedProbeFilterEqualities against the full plan text.
func keyedProbePlanProblems(plan, indexName string, equalities map[string]string) []string {
	var problems []string
	if strings.Contains(plan, "Seq Scan on supply_chain_impact_finding_supports") {
		problems = append(problems, "the plan scans the supports table sequentially")
	}
	var cond []string
	found := false
	for _, line := range strings.Split(plan, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(line, indexName) {
			found = true
			continue
		}
		if found && strings.HasPrefix(trimmed, "Index Cond:") {
			cond = append(cond, trimmed)
		}
	}
	if !found {
		return append(problems, "the plan does not use the keyed probe index")
	}
	if len(cond) == 0 {
		return append(problems, "the index node has no Index Cond")
	}
	conditions := strings.Join(cond, "\n")
	for name, want := range equalities {
		if !strings.Contains(conditions, want) {
			problems = append(problems, "the Index Cond lacks the "+name+" equality")
		}
	}
	return problems
}

const keyedProbePlanSample = `Nested Loop  (cost=9.32..68.00 rows=5 width=68) (actual time=0.042..0.090 rows=10 loops=1)
  ->  Seq Scan on winners_keyed_idents ident  (cost=0.00..1.05 rows=5 width=38) (actual time=0.002..0.002 rows=5 loops=1)
  ->  Bitmap Heap Scan on supply_chain_impact_finding_supports f  (cost=9.32..13.38 rows=1 width=68) (actual time=0.016..0.017 rows=2 loops=5)
        Recheck Cond: ((((payload ->> 'package_id'::text) = ident.package_id) AND (COALESCE(NULLIF((payload ->> 'cve_id'::text), ''::text), NULLIF((payload ->> 'advisory_id'::text), ''::text), ''::text) = ident.c0) AND (COALESCE((payload ->> 'advisory_id'::text), ''::text) = ident.advisory_id)) OR (((payload ->> 'package_id'::text) IS NULL) AND (COALESCE(NULLIF((payload ->> 'cve_id'::text), ''::text), NULLIF((payload ->> 'advisory_id'::text), ''::text), ''::text) = ident.c0) AND (COALESCE((payload ->> 'advisory_id'::text), ''::text) = ident.advisory_id)))
        Filter: ((((payload ->> 'package_id'::text) = ident.package_id) OR ((ident.package_id = ''::text) AND ((payload ->> 'package_id'::text) IS NULL))) AND (((payload ->> 'repository_id'::text) = ident.repository_id) OR ((ident.repository_id = ''::text) AND ((payload ->> 'repository_id'::text) IS NULL))) AND (((payload ->> 'subject_digest'::text) = ident.subject_digest) OR ((ident.subject_digest = ''::text) AND ((payload ->> 'subject_digest'::text) IS NULL))))
        ->  BitmapOr  (cost=9.32..9.32 rows=1 width=0) (actual time=0.013..0.013 rows=0 loops=5)
              ->  Bitmap Index Scan on supply_chain_impact_finding_supports_keyed_probe_idx  (cost=0.00..5.03 rows=1 width=0) (actual time=0.012..0.012 rows=2 loops=5)
                    Index Cond: (((payload ->> 'package_id'::text) = ident.package_id) AND (COALESCE(NULLIF((payload ->> 'cve_id'::text), ''::text), NULLIF((payload ->> 'advisory_id'::text), ''::text), ''::text) = ident.c0) AND (COALESCE((payload ->> 'advisory_id'::text), ''::text) = ident.advisory_id))
              ->  Bitmap Index Scan on supply_chain_impact_finding_supports_keyed_probe_idx  (cost=0.00..4.29 rows=1 width=0) (actual time=0.001..0.001 rows=0 loops=5)
                    Index Cond: (((payload ->> 'package_id'::text) IS NULL) AND (COALESCE(NULLIF((payload ->> 'cve_id'::text), ''::text), NULLIF((payload ->> 'advisory_id'::text), ''::text), ''::text) = ident.c0) AND (COALESCE((payload ->> 'advisory_id'::text), ''::text) = ident.advisory_id))
Planning Time: 0.294 ms`

// TestKeyedProbePlanParserAcceptsTheRealPlanAndRejectsDefects pins the lab plan assertion: the plan
// the lab printed under the ruled #11691 leniency (a BitmapOr over the 354
// index, not one five-column Index Cond) is accepted, and each defect is
// reported: a missing index equality, a Seq Scan on supports, no index use,
// and a leniency arm lost from the Filter.
func TestKeyedProbePlanParserAcceptsTheRealPlanAndRejectsDefects(t *testing.T) {
	t.Parallel()

	if got := keyedProbePlanProblems(keyedProbePlanSample, keyedProbeIndexName, keyedProbeEqualities); len(got) != 0 {
		t.Fatalf("the real plan was rejected: %v", got)
	}
	for name, want := range keyedProbeFilterEqualities {
		if !strings.Contains(keyedProbePlanSample, want) {
			t.Fatalf("the real plan lost the filter equality %s", name)
		}
	}
	droppedArm := strings.ReplaceAll(keyedProbePlanSample, "((payload ->> 'package_id'::text) IS NULL)", "((payload ->> 'package_id'::text) = ''::text)")
	if droppedArm == keyedProbePlanSample {
		t.Fatal("the seeded mutation did not change the plan")
	}
	if got := keyedProbePlanProblems(droppedArm, keyedProbeIndexName, keyedProbeEqualities); len(got) != 1 || !strings.Contains(got[0], "package_id_null") {
		t.Errorf("a plan lacking the package_id IS NULL arm reported %v, want exactly that equality", got)
	}
	droppedFirstArm := strings.Replace(keyedProbePlanSample, "Index Cond: (((payload ->> 'package_id'::text) = ident.package_id)", "Index Cond: (((payload ->> 'package_id'::text) = ident.c0)", 1)
	if droppedFirstArm == keyedProbePlanSample {
		t.Fatal("the seeded mutation did not change the plan")
	}
	if got := keyedProbePlanProblems(droppedFirstArm, keyedProbeIndexName, keyedProbeEqualities); len(got) != 1 || !strings.Contains(got[0], "package_id equality") {
		t.Errorf("a plan lacking the package_id equality reported %v, want exactly that equality", got)
	}
	filterOnly := strings.Replace(keyedProbePlanSample, "Index Cond:", "Filter:", 1)
	if got := keyedProbePlanProblems(filterOnly, keyedProbeIndexName, keyedProbeEqualities); len(got) == 0 {
		t.Error("a plan whose first arm's equalities are only a Filter was accepted")
	}
	seq := strings.Replace(keyedProbePlanSample, "Seq Scan on winners_keyed_idents ident", "Seq Scan on supply_chain_impact_finding_supports ident", 1)
	if got := keyedProbePlanProblems(seq, keyedProbeIndexName, keyedProbeEqualities); len(got) == 0 {
		t.Error("a plan with a Seq Scan on the supports table was accepted")
	}
	if got := keyedProbePlanProblems(strings.ReplaceAll(keyedProbePlanSample, keyedProbeIndexName, "some_other_idx"), keyedProbeIndexName, keyedProbeEqualities); len(got) == 0 {
		t.Error("a plan that does not use the keyed probe index was accepted")
	}
}

// keyedTripleGuardProbeIndexName is the triple-guarded probe index (migration 363).
const keyedTripleGuardProbeIndexName = "supply_chain_impact_finding_supports_keyed_triple_guard_idx"

// keyedTripleGuardProbeEqualities are the five identifier equalities the
// triple-guarded probe applies, as the planner prints them in an Index Cond:
// plain columns, no payload operators.
var keyedTripleGuardProbeEqualities = map[string]string{
	"package_id":      "(package_id = ident.package_id)",
	"repository_id":   "(repository_id = ident.repository_id)",
	"subject_digest":  "(subject_digest = ident.subject_digest)",
	"cve_or_advisory": "COALESCE(NULLIF(cve_id, ''::text), NULLIF(advisory_id, ''::text), ''::text)) = ident.c0",
	"advisory_id":     "(advisory_id = ident.advisory_id)",
}

// keyedTripleGuardProbePlanSample is the measured lenient plan (Refs
// #11691; same shape as the round-6 falsifier plan with the guard Filter
// dropped): an Index Only Scan with 0 heap fetches over the five plain
// equalities, missing-key rows now matched instead of removed.
const keyedTripleGuardProbePlanSample = `Nested Loop  (cost=0.41..1939.62 rows=430 width=61) (actual rows=4199.00 loops=1)
  Buffers: shared hit=456, local hit=2
  ->  Seq Scan on winners_keyed_idents ident  (cost=0.00..14.30 rows=430 width=160) (actual rows=125.00 loops=1)
        Buffers: local hit=2
  ->  Index Only Scan using supply_chain_impact_finding_supports_keyed_triple_guard_idx on supply_chain_impact_finding_supports s  (cost=0.41..4.47 rows=1 width=61) (actual rows=33.59 loops=125)
        Index Cond: ((package_id = ident.package_id) AND (repository_id = ident.repository_id) AND (subject_digest = ident.subject_digest) AND ((COALESCE(NULLIF(cve_id, ''::text), NULLIF(advisory_id, ''::text), ''::text)) = ident.c0) AND (advisory_id = ident.advisory_id))
        Heap Fetches: 0
        Index Searches: 125
        Buffers: shared hit=456
Planning:
  Buffers: shared hit=3
Planning Time: 0.163 ms
Execution Time: 1.265 ms`

// TestKeyedTripleGuardPlanParserAcceptsTheMeasuredPlanAndRejectsDefects pins
// the lab plan assertion for the triple-guarded probe: the measured falsifier
// plan is accepted, and each defect is reported.
func TestKeyedTripleGuardPlanParserAcceptsTheMeasuredPlanAndRejectsDefects(t *testing.T) {
	t.Parallel()

	if got := keyedProbePlanProblems(keyedTripleGuardProbePlanSample, keyedTripleGuardProbeIndexName, keyedTripleGuardProbeEqualities); len(got) != 0 {
		t.Fatalf("the measured plan was rejected: %v", got)
	}
	dropped := strings.Replace(keyedTripleGuardProbePlanSample, " AND (subject_digest = ident.subject_digest)", "", 1)
	if dropped == keyedTripleGuardProbePlanSample {
		t.Fatal("the seeded mutation did not change the plan")
	}
	if got := keyedProbePlanProblems(dropped, keyedTripleGuardProbeIndexName, keyedTripleGuardProbeEqualities); len(got) != 1 || !strings.Contains(got[0], "subject_digest") {
		t.Errorf("a plan lacking the subject_digest equality reported %v, want exactly that equality", got)
	}
	seq := strings.Replace(keyedTripleGuardProbePlanSample, "Index Only Scan using "+keyedTripleGuardProbeIndexName, "Seq Scan on supply_chain_impact_finding_supports", 1)
	if got := keyedProbePlanProblems(seq, keyedTripleGuardProbeIndexName, keyedTripleGuardProbeEqualities); len(got) == 0 {
		t.Error("a plan with a Seq Scan on the supports table was accepted")
	}
	if got := keyedProbePlanProblems(strings.ReplaceAll(keyedTripleGuardProbePlanSample, keyedTripleGuardProbeIndexName, "some_other_idx"), keyedTripleGuardProbeIndexName, keyedTripleGuardProbeEqualities); len(got) == 0 {
		t.Error("a plan that does not use the triple-guard index was accepted")
	}
}
