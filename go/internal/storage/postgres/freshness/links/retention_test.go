// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// countsRows is one result row of int64 columns.
type countsRows struct {
	values []int64
	done   bool
}

func (r *countsRows) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}

func (r *countsRows) Scan(dest ...any) error {
	for i, d := range dest {
		*d.(*int64) = r.values[i]
	}
	return nil
}

func (r *countsRows) Err() error   { return nil }
func (r *countsRows) Close() error { return nil }

// countsQueryer answers every query with one row of fixed counts.
type countsQueryer struct{ values []int64 }

func (q countsQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return &countsRows{values: q.values}, nil
}

// TestDeletePrunedGenerationRowsMismatchNamesTheBatch pins the fail-closed
// error's content (PR #7376 review): when the delete removes fewer rows than
// its own snapshot selected, the error names the batch -- its size and its
// generation ids -- beside the counts, so an operator can tell which
// generation is wedged without re-running queries.
func TestDeletePrunedGenerationRowsMismatchNamesTheBatch(t *testing.T) {
	// links, deltas, buckets, activations deleted; links, deltas, buckets expected.
	q := countsQueryer{values: []int64{1, 90, 1, 1, 1, 100, 1}}
	_, err := DeletePrunedGenerationRows(t.Context(), q, []string{"scope-a", "scope-b"}, []string{"gen-a0", "gen-b3"})
	if err == nil {
		t.Fatal("DeletePrunedGenerationRows succeeded, want the fail-closed mismatch error")
	}
	for _, want := range []string{"2 generations", "gen-a0", "gen-b3", "90 of 100 deltas"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}
