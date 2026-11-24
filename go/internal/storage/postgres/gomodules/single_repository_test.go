// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gomodules

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestSingleRepositoryQueryKeepsRotationPredicates pins the single-repository
// read against the rotation page contract: the same candidate predicates, the
// same go.mod and Go-evidence filters, the same active-generation commit join,
// and a total-candidates count over the same candidate set the page windows.
// A drift here would plan a repository the rotation would never visit, or
// fence with a wrong slot count.
func TestSingleRepositoryQueryKeepsRotationPredicates(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"source_system = 'git'",
		"scope_kind = 'repository'",
		"active_generation_id IS NOT NULL",
		"source_key = $1",
		"relative_path = 'go.mod'",
		"relative_path LIKE '%/go.mod'",
		"vulnerability.go_module_evidence",
		"vulnerability.go_call_reachability",
		"COUNT(*)",
	} {
		if !strings.Contains(singleRepositoryQuery, want) {
			t.Errorf("singleRepositoryQuery is missing %q", want)
		}
	}
	if strings.Contains(singleRepositoryQuery, "$2") {
		t.Error("singleRepositoryQuery takes only the repository id; want no $2 page parameter")
	}
}

// singleRepoRows is a canned db.Rows for GetGoModuleRepository execution
// tests: one []any row per result, assigned positionally into Scan dests.
type singleRepoRows struct {
	rows  [][]any
	index int
	err   error
}

func (r *singleRepoRows) Next() bool   { r.index++; return r.index <= len(r.rows) }
func (r *singleRepoRows) Err() error   { return r.err }
func (r *singleRepoRows) Close() error { return nil }

func (r *singleRepoRows) Scan(dest ...any) error {
	row := r.rows[r.index-1]
	if len(dest) != len(row) {
		return sql.ErrNoRows
	}
	for i, v := range row {
		switch d := dest[i].(type) {
		case *string:
			s, ok := v.(string)
			if !ok {
				return sql.ErrNoRows
			}
			*d = s
		case *bool:
			b, ok := v.(bool)
			if !ok {
				return sql.ErrNoRows
			}
			*d = b
		case *int:
			n, ok := v.(int)
			if !ok {
				return sql.ErrNoRows
			}
			*d = n
		case *sql.NullTime:
			t, ok := v.(sql.NullTime)
			if !ok {
				return sql.ErrNoRows
			}
			*d = t
		default:
			return sql.ErrNoRows
		}
	}
	return nil
}

// singleRepoDB is a canned db.Queryer: query text and repository id are
// recorded, rows or the query error are replayed in order.
type singleRepoDB struct {
	queries []string
	args    [][]any
	rows    [][]any
	err     error
}

func (f *singleRepoDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	f.queries = append(f.queries, query)
	f.args = append(f.args, args)
	if f.err != nil {
		return nil, f.err
	}
	return &singleRepoRows{rows: f.rows}, nil
}

// singleRepoRow builds one GetGoModuleRepository result row in scan order:
// repository id, remote URL, commit, total candidates, has-go-module,
// has-evidence, active generation, active generation row, evidence generation,
// evidence commit, evidence left-at.
func singleRepoRow() []any {
	return []any{
		"repository:r_efdfb126",
		"https://github.com/eshu-hq/repo00000001",
		"0123456789abcdef0123456789abcdef01234567",
		815,
		true,
		true,
		true,
		true,
		"gen-1",
		"fedcba9876543210fedcba9876543210fedcba98",
		sql.NullTime{Time: time.Date(2026, time.November, 23, 4, 7, 52, 0, time.UTC), Valid: true},
	}
}

// TestGetGoModuleRepositoryExecutesTheReaderContract pins the reader's runtime
// behavior without a database: a match returns the read with repository fields
// verbatim, the repository id travels as the only argument, an empty id never
// queries, no rows and generation-less rows map to the not_eligible sentinels,
// a query error wraps, and a duplicate scope id fails closed.
func TestGetGoModuleRepositoryExecutesTheReaderContract(t *testing.T) {
	t.Parallel()

	t.Run("match returns the read verbatim", func(t *testing.T) {
		t.Parallel()
		fake := &singleRepoDB{rows: [][]any{singleRepoRow()}}
		read, err := NewStore(fake).GetGoModuleRepository(context.Background(), "repository:r_efdfb126")
		if err != nil {
			t.Fatalf("GetGoModuleRepository() error = %v", err)
		}
		if read.Repository.RepositoryID != "repository:r_efdfb126" ||
			read.Repository.RemoteURL != "https://github.com/eshu-hq/repo00000001" ||
			read.Repository.SourceCommitSHA != "0123456789abcdef0123456789abcdef01234567" {
			t.Fatalf("repository = %+v, want the fixture scope identity", read.Repository)
		}
		if read.TotalCandidates != 815 || !read.HasGoModule || !read.Repository.HasGoEvidence {
			t.Fatalf("read = %+v, want total 815 with go module and evidence", read)
		}
		if read.Repository.EvidenceGenerationID != "gen-1" {
			t.Fatalf("evidence generation = %q, want gen-1", read.Repository.EvidenceGenerationID)
		}
		if len(fake.args) != 1 || len(fake.args[0]) != 1 || fake.args[0][0] != "repository:r_efdfb126" {
			t.Fatalf("args = %#v, want exactly the repository id", fake.args)
		}
	})

	t.Run("whitespace id never queries", func(t *testing.T) {
		t.Parallel()
		fake := &singleRepoDB{}
		if _, err := NewStore(fake).GetGoModuleRepository(context.Background(), "   "); err != ErrGoModuleRepositoryNotFound {
			t.Fatalf("error = %v, want ErrGoModuleRepositoryNotFound", err)
		}
		if len(fake.queries) != 0 {
			t.Fatalf("queries = %d, want no query for a blank id", len(fake.queries))
		}
	})

	t.Run("no rows maps to not found", func(t *testing.T) {
		t.Parallel()
		fake := &singleRepoDB{}
		if _, err := NewStore(fake).GetGoModuleRepository(context.Background(), "repository:r_missing"); err != ErrGoModuleRepositoryNotFound {
			t.Fatalf("error = %v, want ErrGoModuleRepositoryNotFound", err)
		}
	})

	t.Run("generation-less row maps to no active generation", func(t *testing.T) {
		t.Parallel()
		row := singleRepoRow()
		row[6] = false
		fake := &singleRepoDB{rows: [][]any{row}}
		if _, err := NewStore(fake).GetGoModuleRepository(context.Background(), "repository:r_efdfb126"); err != ErrGoModuleRepositoryNoActiveGeneration {
			t.Fatalf("error = %v, want ErrGoModuleRepositoryNoActiveGeneration", err)
		}
	})

	t.Run("query error wraps", func(t *testing.T) {
		t.Parallel()
		fake := &singleRepoDB{err: context.DeadlineExceeded}
		if _, err := NewStore(fake).GetGoModuleRepository(context.Background(), "repository:r_efdfb126"); err == nil {
			t.Fatal("error = nil, want the wrapped query error")
		}
	})

	t.Run("duplicate scope fails closed", func(t *testing.T) {
		t.Parallel()
		fake := &singleRepoDB{rows: [][]any{singleRepoRow(), singleRepoRow()}}
		if _, err := NewStore(fake).GetGoModuleRepository(context.Background(), "repository:r_efdfb126"); err == nil {
			t.Fatal("error = nil, want the duplicate-scope failure")
		}
	})
}
