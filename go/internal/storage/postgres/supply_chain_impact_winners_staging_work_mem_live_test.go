// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11129 collapse work_mem scoping: the two collapse arms run under the raised transaction-local work_mem, the session value is back before the fold returns, and the session keeps its own setting after the tick commits; a session already above the collapse value keeps it
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// winnersWorkMemProbeTx records current_setting('work_mem') on the tick
// transaction itself immediately before each collapse arm runs, so the test
// observes the setting the arms plan under without changing the fold.
type winnersWorkMemProbeTx struct {
	db.Transaction
	ctx    context.Context
	atArms []string
}

func (p *winnersWorkMemProbeTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if query == collapseWinnersStagingFactSQL || query == collapseWinnersStagingScopeSQL {
		value, err := scanWinnersStagingText(p.ctx, p.Transaction, readWinnersStagingWorkMemSQL, "probe arm work_mem")
		if err != nil {
			return nil, err
		}
		p.atArms = append(p.atArms, value)
	}
	return p.Transaction.QueryContext(ctx, query, args...)
}

// TestSupplyChainImpactWinnersStagingCollapseWorkMemScopedLive pins #11129's
// work_mem scoping on the production fold over one connection: both arms see
// the raised value, the fold restores the session value before it returns,
// and after the tick commits the session still runs its own setting. Removing
// the set_config call fails the first assertion; making it session-wide
// (is_local false) fails the last one.
func TestSupplyChainImpactWinnersStagingCollapseWorkMemScopedLive(t *testing.T) {
	ctx, database := winnersStagingPlanDB(t)
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)
	for _, tc := range []struct {
		session, wantAtArms string
	}{
		{session: "5MB", wantAtArms: winnersStagingCollapseWorkMem},
		{session: "64MB", wantAtArms: "64MB"},
	} {
		t.Run("session_"+tc.session, func(t *testing.T) {
			if _, err := database.ExecContext(ctx, `SET work_mem = '`+tc.session+`'`); err != nil {
				t.Fatalf("set session work_mem: %v", err)
			}
			if _, err := database.ExecContext(ctx, captureWinnersDirtyFactsSQL,
				[]string{"pkg:npm/workmem-" + tc.session}, []string{"repo-workmem"}, []string{""}, []string{"CVE-2026-workmem"}, []string{""}); err != nil {
				t.Fatalf("stage one mark: %v", err)
			}
			inner, err := (SQLDB{DB: database}).Begin(ctx)
			if err != nil {
				t.Fatalf("begin tick: %v", err)
			}
			defer func() { _ = inner.Rollback() }()
			probe := &winnersWorkMemProbeTx{Transaction: inner, ctx: ctx}
			staged, lo, bound, err := readWinnersStagingCollapseBound(ctx, probe, defaultWinnersStagingCollapseMaxRows)
			if err != nil || staged == 0 {
				t.Fatalf("bound read = (%d, %v), want a staged row", staged, err)
			}
			if _, err := foldWinnersStagingRange(ctx, probe, lo, bound); err != nil {
				t.Fatalf("fold: %v", err)
			}
			if got := fmt.Sprint(probe.atArms); got != fmt.Sprint([]string{tc.wantAtArms, tc.wantAtArms}) {
				t.Fatalf("work_mem at the fact and scope arms = %s, want %s for both", got, tc.wantAtArms)
			}
			restored, err := scanWinnersStagingText(ctx, inner, readWinnersStagingWorkMemSQL, "read restored work_mem")
			if err != nil {
				t.Fatalf("read restored work_mem: %v", err)
			}
			if restored != tc.session {
				t.Fatalf("work_mem after the fold, inside the tick = %s, want the session value %s back before the tick clock", restored, tc.session)
			}
			if err := inner.Commit(); err != nil {
				t.Fatalf("commit tick: %v", err)
			}
			var after string
			if err := database.QueryRowContext(ctx, `SHOW work_mem`).Scan(&after); err != nil {
				t.Fatalf("show work_mem after commit: %v", err)
			}
			if after != tc.session {
				t.Fatalf("session work_mem after the tick committed = %s, want %s: the collapse value leaked past its transaction", after, tc.session)
			}
		})
	}
}
