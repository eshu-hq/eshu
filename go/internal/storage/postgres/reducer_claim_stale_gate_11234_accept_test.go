// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// staleGate11234Runs is the number of measured runs per cell
// (arbiter-11234-accept decision 3: six runs, minimum time).
const staleGate11234Runs = 6

// staleGate11234Best runs the chain cut once to warm the cache, then
// staleGate11234Runs times, and returns the minimum execution time and the
// minimum buffers, plus the last plan for the ingestion_scopes figures.
func staleGate11234Best(ctx context.Context, t testing.TB, conn *sql.Conn, query string, domains any) (float64, int64, staleGate11234Plan) {
	t.Helper()
	_ = explainStaleGate11234(ctx, t, conn, nil, query, nil, domains)
	bestMS, bestBuffers := -1.0, int64(-1)
	var last staleGate11234Plan
	for range staleGate11234Runs {
		plan := explainStaleGate11234(ctx, t, conn, nil, query, nil, domains)
		if bestMS < 0 || plan.execMS < bestMS {
			bestMS = plan.execMS
		}
		if bestBuffers < 0 || plan.buffers < bestBuffers {
			bestBuffers = plan.buffers
		}
		last = plan
	}
	return bestMS, bestBuffers, last
}

// TestStaleGate11234AcceptanceLive is the #11234 acceptance matrix on the
// QA-shaped fixture over the full bootstrap schema (arbiter-11234-accept
// decisions 3 and 4): the shipped gate chain against the frozen
// pre-#11234 chain, with fresh ANALYZE and with no statistics, with 0, 200
// and 2,000 stale rows, and the domain filter NULL and set. In every cell
// the shipped chain takes no longer. With fresh statistics (production's
// state) it reads at most one third of the buffers; with none, at most
// 1.25 times, because there the pre-#11234 text is either catastrophic or
// cheap in buffers and slow in CPU, so time ranks the two texts. Figures
// are logged for the evidence note. Operator-gated like the other #11234
// live proofs.
func TestStaleGate11234AcceptanceLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Minute)
	defer cancel()
	preCut := staleGate11234SwapGate(t, staleGate11234ChainCut(), staleGate11234PreGate(t))
	for _, noStats := range []bool{false, true} {
		for _, staleRows := range []int{0, 200, 2000} {
			name := fmt.Sprintf("stats=%v/stale=%d", !noStats, staleRows)
			t.Run(name, func(t *testing.T) {
				sqlConn, conn := openStaleGate11234Live(ctx, t, fmt.Sprintf("acc%d", staleRows), true)
				shape := staleGate11234QAShape()
				shape.staleRows = staleRows
				shape.noStats = noStats
				seedStaleGate11234(ctx, t, conn, shape)
				for _, domains := range []any{nil, staleGate11234Domains[:2]} {
					preMS, preBuffers, _ := staleGate11234Best(ctx, t, sqlConn, preCut, domains)
					gotMS, gotBuffers, plan := staleGate11234Best(ctx, t, sqlConn, staleGate11234ChainCut(), domains)
					t.Logf("%s domains=%v pre: %.1f ms %d buffers; shipped: %.1f ms %d buffers (ingestion_scopes loops=%d buffers=%d, max rescans=%d)",
						name, domains != nil, preMS, preBuffers, gotMS, gotBuffers, plan.scopeLoops, plan.scopeBuffers, plan.maxRescanLoops)
					if gotMS > preMS {
						t.Errorf("%s domains=%v: shipped chain %.1f ms > pre-#11234 %.1f ms", name, domains != nil, gotMS, preMS)
					}
					if !noStats && gotBuffers*3 > preBuffers {
						t.Errorf("%s domains=%v: shipped chain buffers %d > one third of pre-#11234 %d", name, domains != nil, gotBuffers, preBuffers)
					}
					if noStats && gotBuffers*4 > preBuffers*5 {
						t.Errorf("%s domains=%v: shipped chain buffers %d > 1.25x pre-#11234 %d", name, domains != nil, gotBuffers, preBuffers)
					}
				}
			})
		}
	}
}
