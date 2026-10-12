// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// entityUnscopedSnapshotStore is a guarded read store whose snapshot answers
// the unscoped entity search statements from a script. The walk's behavior is
// proved in the unscoped package; these tests prove the reader wires to it and
// decodes through its one metadata decode.
type entityUnscopedSnapshotStore struct {
	db.ReadStore
	tx *entityUnscopedSnapshotTx
}

func (s *entityUnscopedSnapshotStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return s.tx, nil
}

func (s *entityUnscopedSnapshotStore) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	return nil, fmt.Errorf("unbounded read store query reached: %s", query)
}

type entityUnscopedSnapshotTx struct {
	readyErr error
	// hits are the entity rows every window and tail statement returns, in
	// key order; the script answers at most want rows per statement.
	hits [][]any
	// cancelSteps makes every window statement fail with a server cancel, so
	// the walk can only end in a partial result.
	cancelSteps bool
	log         []string
}

func (tx *entityUnscopedSnapshotTx) Commit() error   { return nil }
func (tx *entityUnscopedSnapshotTx) Rollback() error { return nil }
func (tx *entityUnscopedSnapshotTx) QueryRowContext(context.Context, string, ...any) db.Row {
	return nil
}

func (tx *entityUnscopedSnapshotTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	q := strings.TrimSpace(query)
	tx.log = append(tx.log, strings.SplitN(q, "\n", 2)[0])
	switch {
	case strings.Contains(q, "eshu_require_content_substring_indexes_ready"):
		if tx.readyErr != nil {
			return nil, tx.readyErr
		}
		return &entityScriptedRows{pos: -1}, nil
	case strings.Contains(q, "FROM content_entities"):
		if tx.cancelSteps {
			return nil, &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}
		}
		rows := &entityScriptedRows{pos: -1}
		rows.data = append(rows.data, tx.hits...)
		return rows, nil
	}
	return &entityScriptedRows{pos: -1}, nil
}

type entityScriptedRows struct {
	data [][]any
	pos  int
}

func (r *entityScriptedRows) Next() bool   { r.pos++; return r.pos < len(r.data) }
func (r *entityScriptedRows) Err() error   { return nil }
func (r *entityScriptedRows) Close() error { return nil }
func (r *entityScriptedRows) Scan(dest ...any) error {
	for i, d := range dest {
		switch ptr := d.(type) {
		case *string:
			*ptr = r.data[r.pos][i].(string)
		case *int:
			*ptr = int(r.data[r.pos][i].(int64))
		case *[]byte:
			if r.data[r.pos][i] == nil {
				*ptr = nil
			} else {
				*ptr = r.data[r.pos][i].([]byte)
			}
		default:
			return errors.New("entity scripted rows: unsupported scan destination")
		}
	}
	return nil
}

func entityUnscopedHit(id, repo, path string, start int64) []any {
	return []any{
		"hit", id, repo, path, "Function", "name-" + id, start, start + 8, "go",
		"func " + id + "() {}", []byte(`{"kind":"test","body_fp_exact":"strip-me"}`),
	}
}

func TestContentReaderSearchEntitiesUnscopedDecodesRows(t *testing.T) {
	t.Parallel()

	tx := &entityUnscopedSnapshotTx{hits: [][]any{
		entityUnscopedHit("entity-1", "repo-1", "a.go", 10),
		entityUnscopedHit("entity-2", "repo-1", "a.go", 20),
	}}
	reader := NewContentReaderWithReadStore(&entityUnscopedSnapshotStore{tx: tx})

	page, err := reader.SearchEntitiesUnscoped(context.Background(), "render", 5, 0, querycontract.SearchCursor{})
	if err != nil {
		t.Fatalf("SearchEntitiesUnscoped() error = %v", err)
	}
	if len(page.Entities) != 2 || page.More || page.Partial != nil {
		t.Fatalf("page = %+v, want two exact rows", page)
	}
	first := page.Entities[0]
	if first.EntityID != "entity-1" || first.RepoID != "repo-1" || first.RelativePath != "a.go" ||
		first.EntityType != "Function" || first.StartLine != 10 || first.EndLine != 18 ||
		first.Language != "go" || first.SourceCache == "" {
		t.Fatalf("first entity = %+v, want the scripted row payload", first)
	}
	if first.Metadata["kind"] != "test" {
		t.Fatalf("metadata = %v, want the decoded script", first.Metadata)
	}
	if _, stripped := first.Metadata["body_fp_exact"]; stripped {
		t.Fatalf("metadata = %v, want fingerprint keys stripped by the one decode", first.Metadata)
	}
	if !strings.Contains(tx.log[0], "eshu_require_content_substring_indexes_ready") {
		t.Fatalf("first statement = %q, want the readiness check", tx.log[0])
	}
}

func TestContentReaderSearchEntitiesUnscopedClassifiesReadinessFailure(t *testing.T) {
	t.Parallel()

	tx := &entityUnscopedSnapshotTx{readyErr: &pgconn.PgError{Code: "55000", Message: "content substring indexes are not ready"}}
	reader := NewContentReaderWithReadStore(&entityUnscopedSnapshotStore{tx: tx})

	_, err := reader.SearchEntitiesUnscoped(context.Background(), "render", 5, 0, querycontract.SearchCursor{})
	if !errors.Is(err, ErrContentSubstringIndexesNotReady) {
		t.Fatalf("SearchEntitiesUnscoped() error = %v, want the readiness sentinel", err)
	}
}

// The paged seam's no-scope branch used to run the unbounded statement. It now
// goes through the same bounded walk, and a page the budget cut short is an
// error there rather than rows that look complete.
func TestContentReaderSearchEntitiesWithoutScopeIsBounded(t *testing.T) {
	t.Parallel()

	tx := &entityUnscopedSnapshotTx{hits: [][]any{entityUnscopedHit("entity-1", "repo-1", "a.go", 10)}}
	reader := NewContentReaderWithReadStore(&entityUnscopedSnapshotStore{tx: tx})
	rows, err := reader.SearchEntities(context.Background(), "", nil, "render", 5, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("SearchEntities() = (%v, %v), want one row from the bounded walk", rows, err)
	}

	cut := &entityUnscopedSnapshotTx{cancelSteps: true}
	reader = NewContentReaderWithReadStore(&entityUnscopedSnapshotStore{tx: cut}).WithUnscopedSearch(100*time.Millisecond, nil)
	if _, err := reader.SearchEntities(context.Background(), "", nil, "render", 5, 0); !errors.Is(err, ErrUnscopedEntitySearchPartial) {
		t.Fatalf("SearchEntities() error = %v, want ErrUnscopedEntitySearchPartial", err)
	}
}

func TestContentReaderSearchEntitiesUnscopedRejectsBadPattern(t *testing.T) {
	t.Parallel()

	reader := NewContentReaderWithReadStore(&entityUnscopedSnapshotStore{tx: &entityUnscopedSnapshotTx{}})
	if _, err := reader.SearchEntitiesUnscoped(context.Background(), "", 5, 0, querycontract.SearchCursor{}); err == nil {
		t.Fatal("SearchEntitiesUnscoped() with an empty pattern succeeded, want an error")
	}
}
