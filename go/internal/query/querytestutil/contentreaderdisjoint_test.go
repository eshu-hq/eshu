// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"database/sql/driver"
	"testing"
)

// defaultRowsGroup names which of the two default-answer helpers owns a query.
type defaultRowsGroup int

const (
	// factGroup is answered by contentReaderFactDefaultRows.
	factGroup defaultRowsGroup = iota
	// readModelGroup is answered by contentReaderReadModelDefaultRows.
	readModelGroup
)

// defaultRowsCase is one representative query for one group, with the column
// set its own group answers it with.
type defaultRowsCase struct {
	name  string
	query string
	group defaultRowsGroup
	want  []string
}

// defaultRowsCases returns one representative query per default branch across
// both groups. Both tests below walk this same corpus.
//
// Walking one corpus does NOT by itself catch a branch added without a case --
// the tests would iterate the shorter slice and pass, which is under-coverage
// that reads as green. TestContentReaderDefaultGroupsCoverEveryGroupAnsweringBranch
// is what closes that: it counts the answering branches in the source and
// requires one case per branch per group.
func defaultRowsCases() []defaultRowsCase {
	return []defaultRowsCase{
		{
			name:  "workload identity facts",
			query: "SELECT entity_key FROM fact_records WHERE fact_kind = 'reducer_workload_identity'",
			group: factGroup,
			want:  []string{"entity_key"},
		},
		{
			name:  "platform materialization facts",
			query: "SELECT count(*) FROM fact_records WHERE fact_kind = 'reducer_platform_materialization'",
			group: factGroup,
			want:  []string{"count"},
		},
		{
			name: "active fact payloads",
			query: "SELECT fact.payload FROM fact_records AS fact JOIN fact_generations AS generation " +
				"ON generation.id = fact.generation_id WHERE fact.fact_kind = ANY($1::text[]) " +
				"AND generation.status = 'active' AND fact.source_record_id IS NOT NULL",
			group: factGroup,
			want:  []string{"payload"},
		},
		{
			name: "support source-only rollup",
			query: "SELECT COUNT(*) AS support_source_only_count, COUNT(*) AS work_item_source_only_count, " +
				"COUNT(*) AS incident_routing_source_only_count FROM fact_records",
			group: factGroup,
			want: []string{
				"support_source_only_count",
				"work_item_source_only_count",
				"incident_routing_source_only_count",
			},
		},
		{
			name: "documentation source-only rollup",
			query: "SELECT COUNT(*) AS documentation_source_only_count, " +
				"COUNT(*) AS documentation_source_fact_count, COUNT(*) AS documentation_document_fact_count, " +
				"COUNT(*) AS documentation_section_fact_count, COUNT(*) AS documentation_link_fact_count " +
				"FROM fact_records",
			group: factGroup,
			want: []string{
				"documentation_source_only_count",
				"documentation_source_fact_count",
				"documentation_document_fact_count",
				"documentation_section_fact_count",
				"documentation_link_fact_count",
			},
		},
		{
			name:  "deployment evidence",
			query: "WITH scoped_relationships AS (SELECT 1) SELECT r.details FROM scoped_relationships r",
			group: readModelGroup,
			want:  ContentReaderDeploymentEvidenceColumns(),
		},
		{
			name:  "relationship read model",
			query: "WITH scoped_relationships AS (SELECT 1) SELECT r.evidence_count FROM scoped_relationships r",
			group: readModelGroup,
			want:  ContentReaderRelationshipReadModelColumns(),
		},
		{
			name:  "relationship evidence",
			query: "SELECT r.resolved_id FROM resolved_relationships r WHERE r.resolved_id = $1",
			group: readModelGroup,
			want:  ContentReaderRelationshipEvidenceColumns(),
		},
		{
			name:  "resolved relationship count",
			query: "SELECT count(*) FROM resolved_relationships",
			group: readModelGroup,
			want:  []string{"count"},
		},
		{
			name: "shared projection intents",
			query: "SELECT incoming_entity_id FROM shared_projection_intents " +
				"WHERE projection_domain = 'code_calls'",
			group: readModelGroup,
			want:  []string{"incoming_entity_id"},
		},
		{
			name: "dead code candidates",
			query: "SELECT entity_id FROM content_entities WHERE repo_id = $1 AND entity_type = $2 " +
				"ORDER BY entity_id LIMIT $3 OFFSET $4",
			group: readModelGroup,
			want:  ContentReaderDeadCodeCandidateColumns(),
		},
	}
}

// TestContentReaderDefaultGroupsAnswerDisjointQuerySets pins the property that
// makes splitting the default answers across two helpers safe.
//
// The split changed the global branch chain: every fact-group branch now
// evaluates ahead of every read-model branch, where before they were
// interleaved in one function. That reordering is only invisible because no
// query matches a branch in both groups. Nothing else enforces that, and the
// swap is unobservable while it holds -- so the invariant has to be asserted
// directly rather than inferred from a passing suite.
//
// A future branch whose predicate is loose enough to catch the other group's
// queries fails here, naming both the query and the group that wrongly claimed
// it, instead of silently making the answer depend on which helper runs first.
func TestContentReaderDefaultGroupsAnswerDisjointQuerySets(t *testing.T) {
	t.Parallel()

	for _, testCase := range defaultRowsCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			factRows := contentReaderFactDefaultRows(testCase.query, nil)
			readModelRows := contentReaderReadModelDefaultRows(testCase.query, nil)

			owner, other := factRows, readModelRows
			ownerName, otherName := "fact", "read-model"
			if testCase.group == readModelGroup {
				owner, other = readModelRows, factRows
				ownerName, otherName = "read-model", "fact"
			}

			if owner == nil {
				t.Fatalf("%s group did not answer its own query %q", ownerName, testCase.query)
			}
			if other != nil {
				t.Fatalf("%s group also answered a %s-group query %q with columns %v; "+
					"the two groups must stay disjoint or the answer depends on evaluation order",
					otherName, ownerName, testCase.query, other.Columns())
			}
		})
	}
}

// TestContentReaderDefaultGroupsAnswerWithTheirOwnShape covers the other half:
// each branch answers with the column set its read model actually selects.
// Disjointness alone would be satisfied by a branch answering with the wrong
// shape, which surfaces as a scan error inside a handler rather than as a
// harness defect.
func TestContentReaderDefaultGroupsAnswerWithTheirOwnShape(t *testing.T) {
	t.Parallel()

	for _, testCase := range defaultRowsCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var rows driver.Rows
			if testCase.group == factGroup {
				rows = contentReaderFactDefaultRows(testCase.query, nil)
			} else {
				rows = contentReaderReadModelDefaultRows(testCase.query, nil)
			}
			if rows == nil {
				t.Fatalf("no default answer for %q", testCase.query)
			}

			got := rows.Columns()
			if len(got) != len(testCase.want) {
				t.Fatalf("columns = %v, want %v", got, testCase.want)
			}
			for i, want := range testCase.want {
				if got[i] != want {
					t.Fatalf("columns[%d] = %q, want %q (full set %v)", i, got[i], want, got)
				}
			}
		})
	}
}
