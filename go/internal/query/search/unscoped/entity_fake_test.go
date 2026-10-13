// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// fakeEntityRow is one content_entities row in
// (repo_id, relative_path, start_line, entity_id) order.
type fakeEntityRow struct {
	repo, path, typ, name, lang, source string
	start, end                          int
	id                                  string
	metadata                            []byte
	match                               bool
	cost                                time.Duration // cost of testing this row in a key-ordered walk
}

// newEntityCorpus builds n rows with keys that sort in index order: three
// entities per file so the resume cursor must carry start_line and entity_id,
// not just the file key. isMatch picks the matching rows; rowCost is the
// per-row walk cost.
func newEntityCorpus(n int, rowCost time.Duration, isMatch func(i int) bool) *fakeCorpus {
	corpus := &fakeCorpus{tailCost: func(int) time.Duration { return 300 * time.Millisecond }}
	for i := 0; i < n; i++ {
		file := i / 3
		corpus.entities = append(corpus.entities, fakeEntityRow{
			repo:     fmt.Sprintf("repo-%03d", file/100),
			path:     fmt.Sprintf("file-%05d.go", file),
			typ:      "Function",
			name:     fmt.Sprintf("name-%06d", i),
			lang:     "go",
			source:   fmt.Sprintf("func name%06d() {}", i),
			start:    10 + 10*(i%3),
			end:      18 + 10*(i%3),
			id:       fmt.Sprintf("entity-%06d", i),
			metadata: []byte(fmt.Sprintf(`{"seq":%d}`, i)),
			match:    isMatch(i),
			cost:     rowCost,
		})
	}
	return corpus
}

// entityIndexAfter returns the index of the first entity row after the four-part
// cursor. The fake reads the operator from the statement text: a statement that
// spells the predicate ">=" re-reads the cursor row, exactly as PostgreSQL
// would, so a wrong operator in the shipped SQL changes the rows. A statement
// that resumes on the file key alone (repo_id, relative_path) re-reads the
// cursor's whole file, so the fake rejects it: entities sharing a file would
// return twice.
func (c *fakeCorpus) entityIndexAfter(query, repo, path string, start int64, id string) (int, error) {
	if !strings.Contains(query, "(repo_id, relative_path, start_line, entity_id) >") {
		return 0, fmt.Errorf("fake: cursor arguments bound but the statement has no four-part keyset predicate: %s", query)
	}
	if strings.Contains(query, "(repo_id, relative_path, start_line, entity_id) >=") {
		return 0, fmt.Errorf("fake: inclusive four-part keyset predicate re-reads the cursor row: %s", query)
	}
	for i, row := range c.entities {
		if afterEntityKey(row, repo, path, start, id) {
			return i, nil
		}
	}
	return len(c.entities), nil
}

func afterEntityKey(row fakeEntityRow, repo, path string, start int64, id string) bool {
	if row.repo != repo {
		return row.repo > repo
	}
	if row.path != path {
		return row.path > path
	}
	if int64(row.start) != start {
		return int64(row.start) > start
	}
	return row.id > id
}

// runEntityStep simulates the entity key-ordered window statement: $1 pattern,
// $2 window rows, $3 matches wanted, optional $4/$5/$6/$7 cursor.
func (tx *fakeTx) runEntityStep(query string, args []any) (db.Rows, error) {
	window, err := asInt64(args[1])
	if err != nil {
		return nil, err
	}
	want, err := asInt64(args[2])
	if err != nil {
		return nil, err
	}
	from := 0
	if len(args) >= 7 {
		start, err := asInt64(args[5])
		if err != nil {
			return nil, err
		}
		repo, ok := args[3].(string)
		if !ok {
			return nil, fmt.Errorf("fake: cursor repo_id is %T, want string", args[3])
		}
		path, ok := args[4].(string)
		if !ok {
			return nil, fmt.Errorf("fake: cursor relative_path is %T, want string", args[4])
		}
		id, ok := args[6].(string)
		if !ok {
			return nil, fmt.Errorf("fake: cursor entity_id is %T, want string", args[6])
		}
		if from, err = tx.db.corpus.entityIndexAfter(query, repo, path, start, id); err != nil {
			return nil, err
		}
	} else if strings.Contains(query, "(repo_id, relative_path, start_line, entity_id) >") {
		return nil, fmt.Errorf("fake: keyset predicate without cursor arguments: %s", query)
	}
	// The edge row is the window's last row; any other expression moves the
	// cursor off the window.
	edgeBack := 0
	match := edgeOffsetPattern.FindStringSubmatch(query)
	if match == nil {
		return nil, fmt.Errorf("fake: no recognizable edge OFFSET in the statement: %s", query)
	}
	if match[1] != "" {
		edgeBack, _ = strconv.Atoi(match[1])
	}
	rows := tx.db.corpus.entities
	var cost time.Duration
	var out [][]any
	var hits int64
	for i := from; i < from+int(window) && i < len(rows) && hits < want; i++ {
		cost += rows[i].cost
		if tx.timeout > 0 && cost > tx.timeout {
			// The recheck of the row in progress cannot be interrupted, so the
			// cancel lands when that row is done, not at the timeout.
			return tx.cancel(cost - tx.timeout)
		}
		if rows[i].match {
			hits++
			out = append(out, entityRow(kindHit, rows[i]))
		}
	}
	tx.db.clock.advance(cost)
	if edge := from + int(window) - edgeBack; edge >= 0 && edge < len(rows) {
		out = append(out, entityRow(kindEdge, rows[edge]))
	}
	return newFakeRows(out), nil
}

// runEntityTail simulates the entity trigram bitmap statement: $1 pattern, $2
// matches wanted, optional $3/$4/$5/$6 cursor. It refuses to run with index
// scans enabled, because the contract is that the tail is planned as the
// trigram bitmap.
func (tx *fakeTx) runEntityTail(query string, args []any) (db.Rows, error) {
	if !tx.indexOff {
		return nil, fmt.Errorf("fake: tail ran with enable_indexscan on")
	}
	want, err := asInt64(args[1])
	if err != nil {
		return nil, err
	}
	from := 0
	if len(args) >= 6 {
		start, err := asInt64(args[4])
		if err != nil {
			return nil, err
		}
		repo, ok := args[2].(string)
		if !ok {
			return nil, fmt.Errorf("fake: cursor repo_id is %T, want string", args[2])
		}
		path, ok := args[3].(string)
		if !ok {
			return nil, fmt.Errorf("fake: cursor relative_path is %T, want string", args[3])
		}
		id, ok := args[5].(string)
		if !ok {
			return nil, fmt.Errorf("fake: cursor entity_id is %T, want string", args[5])
		}
		if from, err = tx.db.corpus.entityIndexAfter(query, repo, path, start, id); err != nil {
			return nil, err
		}
	} else if strings.Contains(query, "(repo_id, relative_path, start_line, entity_id) >") {
		return nil, fmt.Errorf("fake: keyset predicate without cursor arguments: %s", query)
	}
	cost := tx.db.corpus.tailCost(from)
	if tx.timeout > 0 && cost > tx.timeout {
		return tx.cancel(tx.db.corpus.tailLag)
	}
	tx.db.clock.advance(cost)
	var out [][]any
	for i := from; i < len(tx.db.corpus.entities) && int64(len(out)) < want; i++ {
		if tx.db.corpus.entities[i].match {
			out = append(out, entityRow(kindHit, tx.db.corpus.entities[i]))
		}
	}
	return newFakeRows(out), nil
}

func entityRow(kind string, row fakeEntityRow) []any {
	if kind == kindEdge {
		return []any{kind, row.id, row.repo, row.path, "", "", int64(row.start), int64(0), "", "", nil}
	}
	return []any{kind, row.id, row.repo, row.path, row.typ, row.name, int64(row.start), int64(row.end), row.lang, row.source, row.metadata}
}
