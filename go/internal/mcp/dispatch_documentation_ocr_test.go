// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestDispatchToolListDocumentationFactsPreservesOCRReadback(t *testing.T) {
	t.Parallel()

	db := openMCPDocumentationOCRTestDB(t, []byte(`{
		"fact_id": "fact:ocr-section",
		"fact_kind": "documentation_section",
		"scope_id": "doc-source:git:repo-ocr",
		"generation_id": "gen-ocr-1",
		"payload": {
			"document_id": "doc:git:repo-ocr:docs/architecture.png",
			"section_id": "ocr:title",
			"content": "Architecture dashboard",
			"source_metadata": {
				"format_family": "image_ocr",
				"bounds_x": "0.1000",
				"confidence_bucket": "high"
			}
		}
	}`))
	mux := http.NewServeMux()
	handler := &query.DocumentationHandler{
		Content: query.NewContentReader(db),
		Profile: query.ProfileProduction,
	}
	handler.Mount(mux)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"list_documentation_facts",
		map[string]any{
			"fact_kind":   "documentation_section",
			"source_id":   "doc-source:git:repo-ocr",
			"document_id": "doc:git:repo-ocr:docs/architecture.png",
			"section_id":  "ocr:title",
			"q":           "Architecture",
			"limit":       float64(1),
		},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want documentation facts envelope")
	}
	data := mcpEnvelopeData(t, result)
	rows := mcpMapSliceValue(data, "facts")
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(facts) = %d, want %d", got, want)
	}
	payload := mcpMapValue(rows[0], "payload")
	metadata := mcpMapValue(payload, "source_metadata")
	if got, want := query.StringVal(metadata, "format_family"), "image_ocr"; got != want {
		t.Fatalf("format_family = %q, want %q", got, want)
	}
	if got, want := query.StringVal(metadata, "confidence_bucket"), "high"; got != want {
		t.Fatalf("confidence_bucket = %q, want %q", got, want)
	}
}

func openMCPDocumentationOCRTestDB(t *testing.T, row []byte) *sql.DB {
	t.Helper()

	name := fmt.Sprintf("mcp-doc-ocr-test-%d", atomic.AddUint64(&mcpDocumentationOCRDriverSeq, 1))
	sql.Register(name, mcpDocumentationOCRDriver{row: row})
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

var mcpDocumentationOCRDriverSeq uint64

type mcpDocumentationOCRDriver struct {
	row []byte
}

func (d mcpDocumentationOCRDriver) Open(string) (driver.Conn, error) {
	return &mcpDocumentationOCRConn{row: d.row}, nil
}

type mcpDocumentationOCRConn struct {
	row []byte
}

func (c *mcpDocumentationOCRConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("Prepare not implemented")
}

func (c *mcpDocumentationOCRConn) Close() error {
	return nil
}

func (c *mcpDocumentationOCRConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("Begin not implemented")
}

func (c *mcpDocumentationOCRConn) QueryContext(_ context.Context, queryText string, _ []driver.NamedValue) (driver.Rows, error) {
	for _, want := range []string{
		"FROM fact_records",
		"fact_records.payload->>'source_id'",
		"fact_records.payload->>'document_id'",
		"fact_records.payload->>'section_id'",
		"LOWER(",
	} {
		if !strings.Contains(queryText, want) {
			return nil, fmt.Errorf("documentation OCR query missing %q: %s", want, queryText)
		}
	}
	return &mcpDocumentationOCRRows{row: c.row}, nil
}

var _ driver.QueryerContext = (*mcpDocumentationOCRConn)(nil)

type mcpDocumentationOCRRows struct {
	row  []byte
	done bool
}

func (r *mcpDocumentationOCRRows) Columns() []string {
	return []string{"payload"}
}

func (r *mcpDocumentationOCRRows) Close() error {
	return nil
}

func (r *mcpDocumentationOCRRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	dest[0] = r.row
	r.done = true
	return nil
}
