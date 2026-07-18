// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

type contentReaderQueryResult struct {
	columns              []string
	rows                 [][]driver.Value
	err                  error
	queryContains        []string
	queryContainsInOrder []string
}

func openContentReaderTestDB(t *testing.T, results []contentReaderQueryResult) *sql.DB {
	t.Helper()

	name := fmt.Sprintf("content-reader-test-%d", atomic.AddUint64(&contentReaderDriverSeq, 1))
	sql.Register(name, &contentReaderDriver{results: results})

	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

var contentReaderDriverSeq uint64

type contentReaderDriver struct {
	results []contentReaderQueryResult
}

func (d *contentReaderDriver) Open(string) (driver.Conn, error) {
	return &contentReaderConn{results: append([]contentReaderQueryResult(nil), d.results...)}, nil
}

type contentReaderConn struct {
	results []contentReaderQueryResult
}

func (c *contentReaderConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("Prepare not implemented")
}

func (c *contentReaderConn) Close() error {
	return nil
}

func (c *contentReaderConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("Begin not implemented")
}

func (c *contentReaderConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "SELECT EXISTS") &&
		strings.Contains(query, "FROM content_file_references") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], []string{"available"})) {
		return &contentReaderRows{
			columns: []string{"available"},
			rows:    [][]driver.Value{{false}},
		}, nil
	}
	if strings.Contains(query, "SELECT count(*) FROM content_files WHERE repo_id = $1") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], []string{"count"})) {
		return &contentReaderRows{columns: []string{"count"}, rows: [][]driver.Value{{int64(0)}}}, nil
	}
	if strings.Contains(query, "SELECT count(*) FROM content_entities WHERE repo_id = $1") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], []string{"count"})) {
		return &contentReaderRows{columns: []string{"count"}, rows: [][]driver.Value{{int64(0)}}}, nil
	}
	if strings.Contains(query, "SELECT max(indexed_at) as indexed_at") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], []string{"indexed_at"})) {
		return &contentReaderRows{columns: []string{"indexed_at"}, rows: [][]driver.Value{{nil}}}, nil
	}
	if strings.Contains(query, "SELECT coalesce(language, 'unknown') as language, count(*) as file_count") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], []string{"language", "file_count"})) {
		return &contentReaderRows{columns: []string{"language", "file_count"}, rows: nil}, nil
	}
	if strings.Contains(query, "SELECT entity_type, count(*) as entity_count") &&
		strings.Contains(query, "FROM content_entities") &&
		strings.Contains(query, "GROUP BY entity_type") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], []string{"entity_type", "entity_count"})) {
		return &contentReaderRows{columns: []string{"entity_type", "entity_count"}, rows: nil}, nil
	}
	if strings.Contains(query, "FROM content_entities") &&
		strings.Contains(query, "entity_type = 'Function'") &&
		strings.Contains(query, "entity_name IN") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], []string{"entity_name", "relative_path", "language"})) {
		return &contentReaderRows{columns: []string{"entity_name", "relative_path", "language"}, rows: nil}, nil
	}
	if strings.Contains(query, "FROM ingestion_scopes") &&
		strings.Contains(query, "SELECT scope_id") &&
		strings.Contains(query, "LIMIT 1") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], []string{"scope_id"})) {
		return &contentReaderRows{columns: []string{"scope_id"}, rows: nil}, nil
	}
	if strings.Contains(query, "fact_kind = 'reducer_workload_identity'") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], []string{"entity_key"})) {
		return &contentReaderRows{columns: []string{"entity_key"}, rows: nil}, nil
	}
	if strings.Contains(query, "fact_kind = 'reducer_platform_materialization'") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], []string{"count"})) {
		return &contentReaderRows{columns: []string{"count"}, rows: [][]driver.Value{{int64(0)}}}, nil
	}
	if strings.Contains(query, "WITH scoped_relationships AS") &&
		strings.Contains(query, "r.details") &&
		!strings.Contains(query, "r.evidence_count") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], contentReaderDeploymentEvidenceColumns())) {
		return &contentReaderRows{columns: contentReaderDeploymentEvidenceColumns(), rows: nil}, nil
	}
	if strings.Contains(query, "WITH scoped_relationships AS") &&
		strings.Contains(query, "r.evidence_count") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], contentReaderRelationshipReadModelColumns())) {
		return &contentReaderRows{columns: contentReaderRelationshipReadModelColumns(), rows: nil}, nil
	}
	if strings.Contains(query, "WHERE r.resolved_id = $1") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], contentReaderRelationshipEvidenceColumns())) {
		return &contentReaderRows{columns: contentReaderRelationshipEvidenceColumns(), rows: nil}, nil
	}
	if strings.Contains(query, "FROM resolved_relationships") &&
		strings.Contains(query, "count(") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], []string{"count"})) {
		return &contentReaderRows{columns: []string{"count"}, rows: [][]driver.Value{{int64(0)}}}, nil
	}
	if strings.Contains(query, "FROM shared_projection_intents") &&
		strings.Contains(query, "projection_domain = 'code_calls'") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], []string{"incoming_entity_id"})) {
		return &contentReaderRows{columns: []string{"incoming_entity_id"}, rows: nil}, nil
	}
	if strings.Contains(query, "FROM fact_records AS fact") &&
		strings.Contains(query, "fact.fact_kind = ANY($1::text[])") &&
		strings.Contains(query, "generation.status = 'active'") &&
		strings.Contains(query, "source_record_id") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], []string{"payload"})) {
		return &contentReaderRows{columns: []string{"payload"}, rows: nil}, nil
	}
	if strings.Contains(query, "COUNT(*) AS support_source_only_count") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], []string{
			"support_source_only_count",
			"work_item_source_only_count",
			"incident_routing_source_only_count",
		})) {
		return &contentReaderRows{
			columns: []string{
				"support_source_only_count",
				"work_item_source_only_count",
				"incident_routing_source_only_count",
			},
			rows: [][]driver.Value{{int64(0), int64(0), int64(0)}},
		}, nil
	}
	if strings.Contains(query, "COUNT(*) AS documentation_source_only_count") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], []string{
			"documentation_source_only_count",
			"documentation_source_fact_count",
			"documentation_document_fact_count",
			"documentation_section_fact_count",
			"documentation_link_fact_count",
		})) {
		return &contentReaderRows{
			columns: []string{
				"documentation_source_only_count",
				"documentation_source_fact_count",
				"documentation_document_fact_count",
				"documentation_section_fact_count",
				"documentation_link_fact_count",
			},
			rows: [][]driver.Value{{int64(0), int64(0), int64(0), int64(0), int64(0)}},
		}, nil
	}
	if strings.Contains(query, "FROM content_entities") &&
		strings.Contains(query, "entity_type = $2") &&
		strings.Contains(query, "LIMIT $3 OFFSET $4") &&
		(len(c.results) == 0 || !contentReaderResultColumnsEqual(c.results[0], contentReaderDeadCodeCandidateColumns())) {
		return &contentReaderRows{columns: contentReaderDeadCodeCandidateColumns(), rows: nil}, nil
	}
	if len(c.results) == 0 {
		return nil, fmt.Errorf("unexpected query")
	}
	result := c.results[0]
	c.results = c.results[1:]
	for _, fragment := range result.queryContains {
		if !strings.Contains(query, fragment) {
			return nil, fmt.Errorf("query missing fragment %q", fragment)
		}
	}
	if err := contentReaderQueryContainsInOrder(query, result.queryContainsInOrder); err != nil {
		return nil, err
	}
	if result.err != nil {
		return nil, result.err
	}
	return &contentReaderRows{columns: result.columns, rows: result.rows}, nil
}

func contentReaderQueryContainsInOrder(query string, fragments []string) error {
	offset := 0
	for _, fragment := range fragments {
		index := strings.Index(query[offset:], fragment)
		if index < 0 {
			return fmt.Errorf("query missing ordered fragment %q", fragment)
		}
		offset += index + len(fragment)
	}
	return nil
}

func contentReaderRelationshipReadModelColumns() []string {
	return []string{
		"direction", "relationship_type", "source_repo_id", "source_name",
		"target_repo_id", "target_name", "resolved_id", "generation_id",
		"confidence", "evidence_count", "rationale", "resolution_source", "details",
	}
}

func contentReaderDeploymentEvidenceColumns() []string {
	return []string{
		"direction", "resolved_id", "generation_id", "source_repo_id", "source_name",
		"source_remote_url", "source_scope_id", "target_repo_id", "target_name",
		"target_remote_url", "target_scope_id", "relationship_type", "confidence", "details",
	}
}

func contentReaderRelationshipEvidenceColumns() []string {
	return []string{
		"resolved_id", "generation_id", "source_repo_id", "source_name",
		"source_entity_id", "target_repo_id", "target_name", "target_entity_id",
		"relationship_type", "confidence", "evidence_count", "rationale",
		"resolution_source", "details", "generation_scope", "generation_run_id",
		"generation_status",
	}
}

func contentReaderDeadCodeCandidateColumns() []string {
	return []string{
		"entity_id", "entity_name", "entity_type", "repo_id", "relative_path",
		"language", "start_line", "end_line", "metadata",
	}
}

func contentReaderResultColumnsEqual(result contentReaderQueryResult, columns []string) bool {
	if len(result.columns) != len(columns) {
		return false
	}
	for i, column := range columns {
		if result.columns[i] != column {
			return false
		}
	}
	return true
}

type contentReaderRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *contentReaderRows) Columns() []string {
	return r.columns
}

func (r *contentReaderRows) Close() error {
	return nil
}

func (r *contentReaderRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}
