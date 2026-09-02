// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"database/sql/driver"
	"strings"
)

// contentReaderDefaultRows answers the incidental reads a handler issues on the
// way to the query a test actually asserts on, and returns nil when the query
// is not one of them.
//
// Each branch stands down when the head of the queue declares that read's own
// columns, which is how a test that genuinely asserts on one of these queries
// gets its queued rows instead of the empty default. Matching on the column set
// rather than the SQL text keeps that decision in the test's hands: the test
// says which read it is answering by the shape it declares.
//
// results is read, never consumed. A default answer must leave the queue where
// it was, or every later expectation lines up against the wrong query.
//
// The branches are split across three functions for file length. Relative order
// within each group is preserved; the fact group as a whole now evaluates ahead
// of the read-model group, which is safe only while the two answer disjoint
// query sets.
func contentReaderDefaultRows(query string, results []ContentReaderQueryResult) driver.Rows {
	if strings.Contains(query, "SELECT EXISTS") &&
		strings.Contains(query, "FROM content_file_references") &&
		!contentReaderHeadHasColumns(results, []string{"available"}) {
		return &contentReaderRows{
			columns: []string{"available"},
			rows:    [][]driver.Value{{false}},
		}
	}
	if strings.Contains(query, "SELECT count(*) FROM content_files WHERE repo_id = $1") &&
		!contentReaderHeadHasColumns(results, []string{"count"}) {
		return &contentReaderRows{columns: []string{"count"}, rows: [][]driver.Value{{int64(0)}}}
	}
	if strings.Contains(query, "SELECT count(*) FROM content_entities WHERE repo_id = $1") &&
		!contentReaderHeadHasColumns(results, []string{"count"}) {
		return &contentReaderRows{columns: []string{"count"}, rows: [][]driver.Value{{int64(0)}}}
	}
	if strings.Contains(query, "SELECT max(indexed_at) as indexed_at") &&
		!contentReaderHeadHasColumns(results, []string{"indexed_at"}) {
		return &contentReaderRows{columns: []string{"indexed_at"}, rows: [][]driver.Value{{nil}}}
	}
	if strings.Contains(query, "SELECT coalesce(language, 'unknown') as language, count(*) as file_count") &&
		!contentReaderHeadHasColumns(results, []string{"language", "file_count"}) {
		return &contentReaderRows{columns: []string{"language", "file_count"}, rows: nil}
	}
	if strings.Contains(query, "SELECT entity_type, count(*) as entity_count") &&
		strings.Contains(query, "FROM content_entities") &&
		strings.Contains(query, "GROUP BY entity_type") &&
		!contentReaderHeadHasColumns(results, []string{"entity_type", "entity_count"}) {
		return &contentReaderRows{columns: []string{"entity_type", "entity_count"}, rows: nil}
	}
	if strings.Contains(query, "FROM content_entities") &&
		strings.Contains(query, "entity_type = 'Function'") &&
		strings.Contains(query, "entity_name IN") &&
		!contentReaderHeadHasColumns(results, []string{"entity_name", "relative_path", "language"}) {
		return &contentReaderRows{columns: []string{"entity_name", "relative_path", "language"}, rows: nil}
	}
	if strings.Contains(query, "FROM ingestion_scopes") &&
		strings.Contains(query, "SELECT scope_id") &&
		strings.Contains(query, "LIMIT 1") &&
		!contentReaderHeadHasColumns(results, []string{"scope_id"}) {
		return &contentReaderRows{columns: []string{"scope_id"}, rows: nil}
	}
	if rows := contentReaderFactDefaultRows(query, results); rows != nil {
		return rows
	}
	return contentReaderReadModelDefaultRows(query, results)
}

// contentReaderFactDefaultRows answers the fact-store and rollup reads. It is
// split from contentReaderDefaultRows to keep each file inside the repo's
// length cap.
//
// Relative order within this group is unchanged, but the split did move the
// whole group ahead of the read-model branches it used to be interleaved with.
// That reordering is safe only because the two groups answer disjoint query
// sets -- no query matches a branch in both -- which is the invariant to
// preserve when adding a branch here, not the global chain order.
// TestContentReaderDefaultGroupsAnswerDisjointQuerySets enforces it.
func contentReaderFactDefaultRows(query string, results []ContentReaderQueryResult) driver.Rows {
	if strings.Contains(query, "fact_kind = 'reducer_workload_identity'") &&
		!contentReaderHeadHasColumns(results, []string{"entity_key"}) {
		return &contentReaderRows{columns: []string{"entity_key"}, rows: nil}
	}
	if strings.Contains(query, "fact_kind = 'reducer_platform_materialization'") &&
		!contentReaderHeadHasColumns(results, []string{"count"}) {
		return &contentReaderRows{columns: []string{"count"}, rows: [][]driver.Value{{int64(0)}}}
	}
	if strings.Contains(query, "FROM fact_records AS fact") &&
		strings.Contains(query, "fact.fact_kind = ANY($1::text[])") &&
		strings.Contains(query, "generation.status = 'active'") &&
		strings.Contains(query, "source_record_id") &&
		!contentReaderHeadHasColumns(results, []string{"payload"}) {
		return &contentReaderRows{columns: []string{"payload"}, rows: nil}
	}
	supportOnlyColumns := []string{
		"support_source_only_count",
		"work_item_source_only_count",
		"incident_routing_source_only_count",
	}
	if strings.Contains(query, "COUNT(*) AS support_source_only_count") &&
		!contentReaderHeadHasColumns(results, supportOnlyColumns) {
		return &contentReaderRows{
			columns: supportOnlyColumns,
			rows:    [][]driver.Value{{int64(0), int64(0), int64(0)}},
		}
	}
	documentationColumns := []string{
		"documentation_source_only_count",
		"documentation_source_fact_count",
		"documentation_document_fact_count",
		"documentation_section_fact_count",
		"documentation_link_fact_count",
	}
	if strings.Contains(query, "COUNT(*) AS documentation_source_only_count") &&
		!contentReaderHeadHasColumns(results, documentationColumns) {
		return &contentReaderRows{
			columns: documentationColumns,
			rows:    [][]driver.Value{{int64(0), int64(0), int64(0), int64(0), int64(0)}},
		}
	}
	return nil
}

// contentReaderReadModelDefaultRows answers the relationship, projection, and
// dead-code reads whose shapes come from the exported column helpers. Split
// from contentReaderDefaultRows for file length.
//
// Relative order within this group is unchanged; the group as a whole now runs
// after the fact branches rather than interleaved with them. See
// contentReaderFactDefaultRows for why that is safe and what to preserve.
func contentReaderReadModelDefaultRows(query string, results []ContentReaderQueryResult) driver.Rows {
	if strings.Contains(query, "WITH scoped_relationships AS") &&
		strings.Contains(query, "r.details") &&
		!strings.Contains(query, "r.evidence_count") &&
		!contentReaderHeadHasColumns(results, ContentReaderDeploymentEvidenceColumns()) {
		return &contentReaderRows{columns: ContentReaderDeploymentEvidenceColumns(), rows: nil}
	}
	if strings.Contains(query, "WITH scoped_relationships AS") &&
		strings.Contains(query, "r.evidence_count") &&
		!contentReaderHeadHasColumns(results, ContentReaderRelationshipReadModelColumns()) {
		return &contentReaderRows{columns: ContentReaderRelationshipReadModelColumns(), rows: nil}
	}
	if strings.Contains(query, "WHERE r.resolved_id = $1") &&
		!contentReaderHeadHasColumns(results, ContentReaderRelationshipEvidenceColumns()) {
		return &contentReaderRows{columns: ContentReaderRelationshipEvidenceColumns(), rows: nil}
	}
	if strings.Contains(query, "FROM resolved_relationships") &&
		strings.Contains(query, "count(") &&
		!contentReaderHeadHasColumns(results, []string{"count"}) {
		return &contentReaderRows{columns: []string{"count"}, rows: [][]driver.Value{{int64(0)}}}
	}
	if strings.Contains(query, "FROM shared_projection_intents") &&
		strings.Contains(query, "projection_domain = 'code_calls'") &&
		!contentReaderHeadHasColumns(results, []string{"incoming_entity_id"}) {
		return &contentReaderRows{columns: []string{"incoming_entity_id"}, rows: nil}
	}
	if strings.Contains(query, "FROM content_entities") &&
		strings.Contains(query, "entity_type = $2") &&
		strings.Contains(query, "LIMIT $3 OFFSET $4") &&
		!contentReaderHeadHasColumns(results, ContentReaderDeadCodeCandidateColumns()) {
		return &contentReaderRows{columns: ContentReaderDeadCodeCandidateColumns(), rows: nil}
	}
	return nil
}

// contentReaderHeadHasColumns reports whether the head of the queue declares
// exactly columns. An empty queue declares nothing, so every default branch
// applies -- that is what lets a test queue no results at all and still let a
// handler complete its incidental reads.
func contentReaderHeadHasColumns(results []ContentReaderQueryResult, columns []string) bool {
	if len(results) == 0 {
		return false
	}
	return contentReaderResultColumnsEqual(results[0], columns)
}

// contentReaderResultColumnsEqual reports whether result declares exactly
// columns, in order.
func contentReaderResultColumnsEqual(result ContentReaderQueryResult, columns []string) bool {
	if len(result.Columns) != len(columns) {
		return false
	}
	for i, column := range columns {
		if result.Columns[i] != column {
			return false
		}
	}
	return true
}
