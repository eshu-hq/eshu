// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// stagedRangePredicate matches the seq range a collapse SELECT arm carries.
var stagedRangePredicate = regexp.MustCompile(`seq >= (\$\d+) AND seq <= (\$\d+)`)

// TestCollapseWinnersStagingDeleteCarriesTheSelectRange pins #11129: each
// collapse arm's DELETE bounds deletion to the same [lo, bound] seq range its
// SELECT folds, qualified by the delete alias. The expected predicate is
// derived from the shipped SELECT constant, not hand-copied, so a drift in
// either arm fails. Without the upper bound a delete could remove staged rows
// past the bound that were never merged; without the lower bound the planner
// loses the seq-index range scan.
func TestCollapseWinnersStagingDeleteCarriesTheSelectRange(t *testing.T) {
	t.Parallel()

	arms := []struct {
		name      string
		selectSQL string
		fullSQL   string
	}{
		{"fact", collapseWinnersStagingFactSelectSQL, collapseWinnersStagingFactSQL},
		{"scope", collapseWinnersStagingScopeSelectSQL, collapseWinnersStagingScopeSQL},
	}
	for _, arm := range arms {
		t.Run(arm.name, func(t *testing.T) {
			t.Parallel()

			m := stagedRangePredicate.FindStringSubmatch(arm.selectSQL)
			if m == nil {
				t.Fatalf("SELECT arm carries no seq range predicate:\n%s", arm.selectSQL)
			}
			if m[1] != "$1" || m[2] != "$2" {
				t.Errorf("SELECT range parameters = (%s, %s), want ($1, $2): the fold passes (lo, bound)", m[1], m[2])
			}
			_, deleteArm, found := strings.Cut(arm.fullSQL, "DELETE FROM")
			if !found {
				t.Fatalf("collapse arm has no DELETE:\n%s", arm.fullSQL)
			}
			want := "s.seq >= " + m[1] + " AND s.seq <= " + m[2]
			if !strings.Contains(deleteArm, want) {
				t.Errorf("DELETE does not carry the SELECT's seq range %q:\n%s", want, deleteArm)
			}
			if got := strings.Count(arm.fullSQL, "DELETE FROM"); got != 1 {
				t.Errorf("collapse arm has %d DELETE statements, want 1", got)
			}
		})
	}
}

// collapseFakeTx scripts one collapse: the bound read returns the scripted
// (count, lo, bound) and every statement records its arguments, so a test can
// pin the budget the bound walk takes and the range both arms fold.
type collapseFakeTx struct {
	staged, lo, bound int64
	calls             []collapseFakeCall
}

type collapseFakeCall struct {
	query string
	args  []any
}

func (tx *collapseFakeTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return phaseFakeResult{}, nil
}

func (tx *collapseFakeTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	tx.calls = append(tx.calls, collapseFakeCall{query: query, args: args})
	switch query {
	case selectWinnersStagingCollapseBoundSQL:
		return &phaseFakeRows{row: []any{tx.staged, tx.lo, tx.bound}}, nil
	case readWinnersStagingWorkMemSQL, restoreWinnersStagingWorkMemSQL:
		return &phaseFakeRows{row: []any{"4MB"}}, nil
	case setWinnersStagingWorkMemSQL:
		return &phaseFakeRows{row: []any{winnersStagingCollapseWorkMem}}, nil
	case collapseWinnersStagingFactSQL, collapseWinnersStagingScopeSQL:
		return &phaseFakeRows{row: []any{int64(1), int64(1)}}, nil
	}
	return nil, fmt.Errorf("unscripted query %q", query)
}

func (tx *collapseFakeTx) Commit() error   { return nil }
func (tx *collapseFakeTx) Rollback() error { return nil }

// TestCollapseWinnersStagingFoldsTheBoundedRange pins the bound walk and the
// range handoff: the bound read takes the budget as its only argument and its
// SQL walks LIMIT budget+1; when the walk sees more than the budget the wave
// is split and both arms fold exactly [lo, bound]; at or under the budget the
// wave is not split.
func TestCollapseWinnersStagingFoldsTheBoundedRange(t *testing.T) {
	t.Parallel()

	if !strings.Contains(selectWinnersStagingCollapseBoundSQL, "LIMIT $1 + 1") {
		t.Errorf("bound walk does not read at most budget+1 entries:\n%s", selectWinnersStagingCollapseBoundSQL)
	}
	tests := []struct {
		name      string
		staged    int64
		wantSplit bool
	}{
		{"past the budget splits the wave", 4, true},
		{"at the budget folds the whole queue", 3, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tx := &collapseFakeTx{staged: tt.staged, lo: 5, bound: 9}
			outcome, err := collapseWinnersStaging(context.Background(), tx, 3)
			if err != nil {
				t.Fatalf("collapseWinnersStaging() error = %v", err)
			}
			if outcome.split != tt.wantSplit {
				t.Errorf("split = %v, want %v", outcome.split, tt.wantSplit)
			}
			var armArgs [][]any
			for _, call := range tx.calls {
				switch call.query {
				case selectWinnersStagingCollapseBoundSQL:
					if want := []any{int64(3)}; !reflect.DeepEqual(call.args, want) {
						t.Errorf("bound read args = %v, want %v", call.args, want)
					}
				case collapseWinnersStagingFactSQL, collapseWinnersStagingScopeSQL:
					armArgs = append(armArgs, call.args)
				}
			}
			want := [][]any{{int64(5), int64(9)}, {int64(5), int64(9)}}
			if !reflect.DeepEqual(armArgs, want) {
				t.Errorf("collapse arms folded %v, want both arms over [lo, bound] %v", armArgs, want)
			}
		})
	}
}
