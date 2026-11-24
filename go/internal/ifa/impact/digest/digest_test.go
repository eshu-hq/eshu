// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package digest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDriver answers the two statements Read issues from a script, so Read's refusal of an empty family and
// its digest pass-through run without a database.
type fakeDriver struct{ supports, winners int64 }

func (d fakeDriver) Open(string) (driver.Conn, error) { return fakeConn(d), nil }

type fakeConn fakeDriver

func (c fakeConn) Prepare(query string) (driver.Stmt, error) { return fakeStmt{c: c, q: query}, nil }
func (fakeConn) Close() error                                { return nil }
func (fakeConn) Begin() (driver.Tx, error)                   { return nil, io.EOF }

type fakeStmt struct {
	c fakeConn
	q string
}

func (fakeStmt) Close() error  { return nil }
func (fakeStmt) NumInput() int { return 0 }
func (s fakeStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, io.EOF
}

func (s fakeStmt) Query([]driver.Value) (driver.Rows, error) {
	if s.q == DigestSQL {
		return &fakeRows{cols: []string{"digest"}, row: []driver.Value{"abc123"}}, nil
	}
	return &fakeRows{cols: []string{"s", "w"}, row: []driver.Value{s.c.supports, s.c.winners}}, nil
}

type fakeRows struct {
	cols []string
	row  []driver.Value
	done bool
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dest, r.row)
	return nil
}

func openFake(t *testing.T, name string, d fakeDriver) *sql.DB {
	t.Helper()
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("open fake db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestReadRefusesAnEmptyFamily is the seeded red: a family that derived no support or no winner must not be
// digested, so two empty runs can never compare equal.
func TestReadRefusesAnEmptyFamily(t *testing.T) {
	t.Parallel()

	for name, d := range map[string]fakeDriver{
		"no supports": {supports: 0, winners: 2},
		"no winners":  {supports: 3, winners: 0},
		"neither":     {},
	} {
		db := openFake(t, "impactdigest-empty-"+strings.ReplaceAll(name, " ", "-"), d)
		if _, err := Read(context.Background(), db); err == nil {
			t.Fatalf("%s: Read() error = nil, want the empty-family refusal", name)
		}
	}
	db := openFake(t, "impactdigest-full", fakeDriver{supports: 3, winners: 2})
	got, err := Read(context.Background(), db)
	if err != nil || got != (Result{Digest: "abc123", Supports: 3, Winners: 2}) {
		t.Fatalf("Read() = %+v, %v, want the digest and both counts", got, err)
	}
}

// TestDigestExcludesTheRunNamingColumns pins what the digest leaves out: a support payload's intent_id and cause
// and a winner's materialization stamp name the delivering run, not the finding.
func TestDigestExcludesTheRunNamingColumns(t *testing.T) {
	t.Parallel()

	for _, want := range []string{"s.payload - 'intent_id' - 'cause'", "to_jsonb(w) - 'materialized_at'", "ORDER BY s.fact_id", "ORDER BY w.canonical_key"} {
		if !strings.Contains(DigestSQL, want) {
			t.Fatalf("DigestSQL lost %q: equal runs would hash differently", want)
		}
	}
}

// TestCassettesAreCommittedFiles pins that every cassette the family drives exists.
func TestCassettesAreCommittedFiles(t *testing.T) {
	t.Parallel()

	for _, relative := range Cassettes {
		path := filepath.Join("..", "..", "..", "..", "..", "testdata", "cassettes", relative)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("family cassette %s: %v", relative, err)
		}
	}
}

// TestPinnedSkipsCommentsAndBlankLines pins the pin file grammar: the first line that is neither blank nor a
// comment is the digest, and a file with none pins nothing.
func TestPinnedSkipsCommentsAndBlankLines(t *testing.T) {
	t.Parallel()

	for content, want := range map[string]string{
		"# note\n\n  \n":               "",
		"":                             "",
		"# note\nabc123\n# trailing\n": "abc123",
		"  def456  \nabc123\n":         "def456",
	} {
		if got := Pinned(content); got != want {
			t.Fatalf("Pinned(%q) = %q, want %q", content, got, want)
		}
	}
}

// TestShippedPinFileParses pins that the committed pin file exists and parses (it may pin nothing yet).
func TestShippedPinFileParses(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "testdata", "golden", "ifa-impact-family-digest.txt"))
	if err != nil {
		t.Fatalf("read the pin file: %v", err)
	}
	if pin := Pinned(string(raw)); pin != "" && len(pin) != 64 {
		t.Fatalf("pinned digest %q is not a 64-character sha256 hex", pin)
	}
}
