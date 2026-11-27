// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"strings"
	"testing"
)

// keyedProbeIndexName is the draft keyed probe index (migration 354).
const keyedProbeIndexName = "supply_chain_impact_finding_supports_keyed_probe_idx"

// keyedProbeEqualities are the five identifier equalities the keyed probe applies, as the planner prints
// them in an Index Cond (the operator is ->>, which contains the text "->").
var keyedProbeEqualities = map[string]string{
	"package_id":      "(payload ->> 'package_id'::text) = ident.package_id",
	"repository_id":   "(payload ->> 'repository_id'::text) = ident.repository_id",
	"subject_digest":  "(payload ->> 'subject_digest'::text) = ident.subject_digest",
	"cve_or_advisory": "NULLIF((payload ->> 'cve_id'::text), ''::text), NULLIF((payload ->> 'advisory_id'::text), ''::text), ''::text) = ident.c0",
	"advisory_id":     "COALESCE((payload ->> 'advisory_id'::text), ''::text) = ident.advisory_id",
}

// keyedProbeIndexNode returns the text of the plan node that uses the named
// index: the node's own line and the attribute lines under it, up to the next
// plan node. A plan-node marker is "->" as the first non-space text of a line;
// the ->> operator inside a condition is not one.
func keyedProbeIndexNode(plan, indexName string) (string, bool) {
	var node []string
	inNode := false
	for _, line := range strings.Split(plan, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inNode {
			if strings.Contains(line, indexName) {
				inNode = true
				node = append(node, trimmed)
			}
			continue
		}
		if strings.HasPrefix(trimmed, "->") {
			break
		}
		node = append(node, trimmed)
	}
	return strings.Join(node, "\n"), inNode
}

// keyedProbePlanProblems lists what is wrong with a keyed probe plan: no use of the named index, a Seq Scan on
// the supports table, or an equality missing from the index node's Index Cond.
func keyedProbePlanProblems(plan, indexName string, equalities map[string]string) []string {
	var problems []string
	if strings.Contains(plan, "Seq Scan on supply_chain_impact_finding_supports") {
		problems = append(problems, "the plan scans the supports table sequentially")
	}
	node, found := keyedProbeIndexNode(plan, indexName)
	if !found {
		return append(problems, "the plan does not use the keyed probe index")
	}
	var cond []string
	for _, line := range strings.Split(node, "\n") {
		if strings.HasPrefix(line, "Index Cond:") {
			cond = append(cond, line)
		}
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

const keyedProbePlanSample = `Nested Loop  (cost=0.43..61.20 rows=5 width=1024) (actual time=0.05..0.31 rows=5 loops=1)
  ->  Seq Scan on winners_keyed_idents ident  (cost=0.00..1.05 rows=5 width=96) (actual time=0.01..0.01 rows=5 loops=1)
  ->  Index Scan using supply_chain_impact_finding_supports_keyed_probe_idx on supply_chain_impact_finding_supports f  (cost=0.43..12.20 rows=1 width=1024) (actual time=0.04..0.05 rows=1 loops=5)
        Index Cond: (((payload ->> 'package_id'::text) = ident.package_id) AND ((payload ->> 'repository_id'::text) = ident.repository_id) AND ((payload ->> 'subject_digest'::text) = ident.subject_digest) AND (COALESCE(NULLIF((payload ->> 'cve_id'::text), ''::text), NULLIF((payload ->> 'advisory_id'::text), ''::text), ''::text) = ident.c0) AND (COALESCE((payload ->> 'advisory_id'::text), ''::text) = ident.advisory_id))
        Buffers: shared hit=24
Planning Time: 0.4 ms`

// TestKeyedProbePlanParserAcceptsTheRealPlanAndRejectsDefects pins the lab plan assertion: the plan
// the lab printed (with ->> operators in the Index Cond, which an earlier parser took for the next plan
// node) is accepted, and each defect is reported: a missing equality, a Seq Scan on supports, no index use, and
// an equality that is only a Filter.
func TestKeyedProbePlanParserAcceptsTheRealPlanAndRejectsDefects(t *testing.T) {
	t.Parallel()

	if got := keyedProbePlanProblems(keyedProbePlanSample, keyedProbeIndexName, keyedProbeEqualities); len(got) != 0 {
		t.Fatalf("the real plan was rejected: %v", got)
	}
	dropped := strings.Replace(keyedProbePlanSample, " AND ((payload ->> 'subject_digest'::text) = ident.subject_digest)", "", 1)
	if dropped == keyedProbePlanSample {
		t.Fatal("the seeded mutation did not change the plan")
	}
	if got := keyedProbePlanProblems(dropped, keyedProbeIndexName, keyedProbeEqualities); len(got) != 1 || !strings.Contains(got[0], "subject_digest") {
		t.Errorf("a plan lacking the subject_digest equality reported %v, want exactly that equality", got)
	}
	filterOnly := strings.Replace(keyedProbePlanSample, "Index Cond:", "Filter:", 1)
	if got := keyedProbePlanProblems(filterOnly, keyedProbeIndexName, keyedProbeEqualities); len(got) == 0 {
		t.Error("a plan whose equalities are only a Filter was accepted")
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

// keyedTripleGuardProbePlanSample is the measured round-6 falsifier plan
// (12k supports / 125 idents, scratch index renamed to the shipped one): an
// Index Only Scan with 0 heap fetches, the guards as a Filter removing the
// missing-key rows the shipped probe also rejects.
const keyedTripleGuardProbePlanSample = `Nested Loop  (cost=0.41..1939.62 rows=430 width=61) (actual rows=4199.00 loops=1)
  Buffers: shared hit=456, local hit=2
  ->  Seq Scan on winners_keyed_idents ident  (cost=0.00..14.30 rows=430 width=160) (actual rows=125.00 loops=1)
        Buffers: local hit=2
  ->  Index Only Scan using supply_chain_impact_finding_supports_keyed_triple_guard_idx on supply_chain_impact_finding_supports s  (cost=0.41..4.47 rows=1 width=61) (actual rows=33.59 loops=125)
        Index Cond: ((package_id = ident.package_id) AND (repository_id = ident.repository_id) AND (subject_digest = ident.subject_digest) AND ((COALESCE(NULLIF(cve_id, ''::text), NULLIF(advisory_id, ''::text), ''::text)) = ident.c0) AND (advisory_id = ident.advisory_id))
        Filter: (((ident.package_id <> ''::text) OR (((key_presence)::integer & 1) <> 0)) AND ((ident.repository_id <> ''::text) OR (((key_presence)::integer & 2) <> 0)) AND ((ident.subject_digest <> ''::text) OR (((key_presence)::integer & 4) <> 0)))
        Rows Removed by Filter: 5
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
