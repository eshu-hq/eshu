// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// unscopedSnapshotStore is a guarded read store whose snapshot answers the
// unscoped search statements from a script. The walk's behavior is proved in
// the unscoped package; these tests prove the reader wires to it.
type unscopedSnapshotStore struct {
	db.ReadStore
	tx *unscopedSnapshotTx
}

func (s *unscopedSnapshotStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return s.tx, nil
}

func (s *unscopedSnapshotStore) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	return nil, fmt.Errorf("unbounded read store query reached: %s", query)
}

type unscopedSnapshotTx struct {
	readyErr error
	hitKeys  []string
	log      []string
	// cancelSteps makes every window statement fail with a server cancel, so
	// the walk can only end in a partial result.
	cancelSteps bool
}

func (tx *unscopedSnapshotTx) Commit() error   { return nil }
func (tx *unscopedSnapshotTx) Rollback() error { return nil }
func (tx *unscopedSnapshotTx) QueryRowContext(context.Context, string, ...any) db.Row {
	return nil
}

func (tx *unscopedSnapshotTx) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	q := strings.TrimSpace(query)
	tx.log = append(tx.log, strings.SplitN(q, "\n", 2)[0])
	switch {
	case strings.Contains(q, "eshu_require_content_substring_indexes_ready"):
		if tx.readyErr != nil {
			return nil, tx.readyErr
		}
		return &scriptedRows{pos: -1}, nil
	case strings.Contains(q, "FROM content_files"):
		if tx.cancelSteps {
			return nil, &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}
		}
		rows := &scriptedRows{pos: -1}
		for _, key := range tx.hitKeys {
			rows.data = append(rows.data, []any{"hit", "repo-1", key, "sha", "", "hash", int64(3), "go", "source"})
		}
		return rows, nil
	}
	return &scriptedRows{pos: -1}, nil
}

type scriptedRows struct {
	data [][]any
	pos  int
}

func (r *scriptedRows) Next() bool   { r.pos++; return r.pos < len(r.data) }
func (r *scriptedRows) Err() error   { return nil }
func (r *scriptedRows) Close() error { return nil }
func (r *scriptedRows) Scan(dest ...any) error {
	for i, d := range dest {
		switch ptr := d.(type) {
		case *string:
			*ptr = r.data[r.pos][i].(string)
		case *int:
			*ptr = int(r.data[r.pos][i].(int64))
		}
	}
	return nil
}

func TestContentReaderSearchFilesUnscopedUsesBoundedSnapshot(t *testing.T) {
	t.Parallel()

	tx := &unscopedSnapshotTx{hitKeys: []string{"a.go", "b.go"}}
	reader := NewContentReaderWithReadStore(&unscopedSnapshotStore{tx: tx})

	page, err := reader.SearchFilesUnscoped(context.Background(), "render", 5, 0, "", "")
	if err != nil {
		t.Fatalf("SearchFilesUnscoped() error = %v", err)
	}
	if len(page.Files) != 2 || page.More || page.Partial != nil {
		t.Fatalf("page = %+v, want two exact rows", page)
	}
	if !strings.Contains(tx.log[0], "eshu_require_content_substring_indexes_ready") {
		t.Fatalf("first statement = %q, want the readiness check", tx.log[0])
	}
}

func TestContentReaderSearchFilesUnscopedClassifiesReadinessFailure(t *testing.T) {
	t.Parallel()

	tx := &unscopedSnapshotTx{readyErr: &pgconn.PgError{Code: "55000", Message: "content substring indexes are not ready"}}
	reader := NewContentReaderWithReadStore(&unscopedSnapshotStore{tx: tx})

	_, err := reader.SearchFilesUnscoped(context.Background(), "render", 5, 0, "", "")
	if !errors.Is(err, ErrContentSubstringIndexesNotReady) {
		t.Fatalf("SearchFilesUnscoped() error = %v, want the readiness sentinel", err)
	}
}

// The paged seam's no-scope branch used to run the unbounded statement. It now
// goes through the same bounded walk, and a page the budget cut short is an
// error there rather than rows that look complete.
func TestContentReaderSearchFilesWithoutScopeIsBounded(t *testing.T) {
	t.Parallel()

	tx := &unscopedSnapshotTx{hitKeys: []string{"a.go"}}
	reader := NewContentReaderWithReadStore(&unscopedSnapshotStore{tx: tx})
	rows, err := reader.SearchFiles(context.Background(), "", nil, "render", 5, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("SearchFiles() = (%v, %v), want one row from the bounded walk", rows, err)
	}

	cut := &unscopedSnapshotTx{cancelSteps: true}
	reader = NewContentReaderWithReadStore(&unscopedSnapshotStore{tx: cut}).WithUnscopedSearch(100*time.Millisecond, nil)
	if _, err := reader.SearchFiles(context.Background(), "", nil, "render", 5, 0); !errors.Is(err, ErrUnscopedSearchPartial) {
		t.Fatalf("SearchFiles() error = %v, want ErrUnscopedSearchPartial", err)
	}
}

// TestContentHandlerUnscopedSearchEndToEndPartial drives the real handler over
// the real reader and bounded walk: when every window and the tail are
// cancelled by the server, the caller gets HTTP 200, truncated, and a partial
// truth level with a reason, not an error and not an empty complete page.
func TestContentHandlerUnscopedSearchEndToEndPartial(t *testing.T) {
	t.Parallel()

	tx := &unscopedSnapshotTx{cancelSteps: true}
	reader := NewContentReaderWithReadStore(&unscopedSnapshotStore{tx: tx}).WithUnscopedSearch(100*time.Millisecond, nil)
	handler := &ContentHandler{Content: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/content/files/search", strings.NewReader(`{"query":"render"}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data struct {
			Truncated bool `json:"truncated"`
			Partial   *struct {
				Reason string `json:"reason"`
			} `json:"partial"`
		} `json:"data"`
		Truth struct {
			Level string `json:"level"`
		} `json:"truth"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !env.Data.Truncated || env.Data.Partial == nil || env.Truth.Level != "partial" {
		t.Fatalf("envelope = %s, want truncated, partial, truth level partial", rec.Body.String())
	}
}
