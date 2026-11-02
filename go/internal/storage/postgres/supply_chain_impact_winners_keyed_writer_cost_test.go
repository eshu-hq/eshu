// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// winnersWriterCostEnv turns on the H4 writer-cost measurement inside the scale
// proof: the number of interleaved rounds (the ruling asks for five).
const winnersWriterCostEnv = "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_WRITER_COST_ROUNDS"

// winnersScaleKnob reads one scale-proof knob. It lives here, in a file that
// opens no proof database, because the enrollment guard
// (supply_chain_impact_gate_enrollment_test.go) rejects any environment read in
// a proof file so that the DSN is only ever read through
// supplyChainImpactLiveDSN. A knob is not a DSN.
func winnersScaleKnob(name string) string { return os.Getenv(name) }

func winnersWriterCostRounds(t *testing.T) int {
	t.Helper()
	raw := winnersScaleKnob(winnersWriterCostEnv)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		t.Fatalf("%s=%q: want a positive integer", winnersWriterCostEnv, raw)
	}
	return n
}

// timedCaptureTx wraps the production writer transaction and times the capture
// statement. The embedded SupplyChainImpactTx has no capture method, so the
// wrapper itself decides whether the writer sees a capturer: the capture arm
// uses the production transaction as is, the control arm hides it.
type timedCaptureTx struct {
	supplychaincore.SupplyChainImpactTx
	capturer supplychaincore.SupplyChainImpactWinnersCapturer
	spent    *time.Duration
	rows     *int64
}

func (t timedCaptureTx) CaptureWinnersDirtyFacts(ctx context.Context, ids []supplychaincore.WinnersDirtyIdentifier) (int64, error) {
	start := time.Now()
	n, err := t.capturer.CaptureWinnersDirtyFacts(ctx, ids)
	*t.spent += time.Since(start)
	*t.rows += n
	return n, err
}

func (t timedCaptureTx) RecordWinnersCapture(ctx context.Context, source string, rows int64) {
	t.capturer.RecordWinnersCapture(ctx, source, rows)
}

// writerCostBeginner opens production transactions. With capture on it returns
// a timing wrapper that exposes the capturer; with capture off it returns the
// transaction with the capturer hidden, so the writer issues no capture
// statement at all.
type writerCostBeginner struct {
	inner   postgres.SupplyChainImpactBeginner
	capture bool
	spent   *time.Duration
	rows    *int64
}

type hiddenCapturerTx struct {
	supplychaincore.SupplyChainImpactTx
}

func (b writerCostBeginner) BeginSupplyChainImpactTx(ctx context.Context) (supplychaincore.SupplyChainImpactTx, error) {
	tx, err := b.inner.BeginSupplyChainImpactTx(ctx)
	if err != nil {
		return nil, err
	}
	if !b.capture {
		return hiddenCapturerTx{tx}, nil
	}
	capturer, ok := tx.(supplychaincore.SupplyChainImpactWinnersCapturer)
	if !ok {
		return nil, fmt.Errorf("production transaction %T is not a winners capturer", tx)
	}
	return timedCaptureTx{SupplyChainImpactTx: tx, capturer: capturer, spent: b.spent, rows: b.rows}, nil
}

// runWinnersWriterCost measures the direct cost of the capture INSERT on the
// writer path at 1% changed. Each round writes one scope per arm: every
// finding of the scope is sent, one in a hundred carries a new priority, so
// about 1% of the rows are content-changed and the rest are digest-unchanged
// (the production replay shape). The arms alternate which one goes first, and
// swap scopes every round, so neither order nor scope favors one arm. It
// reports the whole writer pass for each arm and the capture statement alone.
func runWinnersWriterCost(h *keyedHarness, corpus winnersScaleCorpus, rounds int) string {
	h.t.Helper()
	// Scopes 0, 100, ... carry the scale test's churn rewrite, so stay off them.
	scopeA, scopeB := 1, min(corpus.scopes/2+1, corpus.scopes-1)
	type sample struct{ pass, capture time.Duration }
	on, off := make([]sample, 0, rounds), make([]sample, 0, rounds)
	var capturedRows int64
	for r := 1; r <= rounds; r++ {
		scopes := map[bool]int{true: scopeA, false: scopeB}
		if r%2 == 0 {
			scopes = map[bool]int{true: scopeB, false: scopeA}
		}
		order := []bool{true, false}
		if r%2 == 0 {
			order = []bool{false, true}
		}
		for _, capture := range order {
			var spent time.Duration
			var rows int64
			writer := h.writer
			writer.DB = writerCostBeginner{
				inner:   postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: h.db}},
				capture: capture, spent: &spent, rows: &rows,
			}
			s := scopes[capture]
			findings := make([]supplychaincore.SupplyChainImpactFinding, 0, winnersScaleScopeSize)
			for i := 0; i < winnersScaleScopeSize; i++ {
				b := 0
				if i%100 == 0 {
					b = 2000 + r*20
					if capture {
						b += 10
					}
				}
				findings = append(findings, corpus.finding(s*winnersScaleScopeSize+i, b))
			}
			start := time.Now()
			if _, err := writer.WriteSupplyChainImpactFindings(h.ctx, supplychaincore.SupplyChainImpactWrite{
				IntentID: "intent-9805-h4", ScopeID: corpus.scopeID(s), GenerationID: corpus.scopeID(s) + "/g1",
				SourceSystem: "vulnerability_intelligence", Cause: "9805 writer cost",
				Findings: findings, FencingToken: keyedFenceTokens.Add(1),
			}); err != nil {
				h.t.Fatalf("writer cost round %d: %v", r, err)
			}
			elapsed := time.Since(start)
			if capture {
				on = append(on, sample{elapsed, spent})
				capturedRows += rows
			} else {
				off = append(off, sample{elapsed, 0})
			}
		}
	}
	if capturedRows == 0 {
		h.t.Fatalf("the capture arm appended no dirty-log rows: the measurement did not exercise the capture statement")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "writer_cost rounds=%d scope_findings=%d changed_per_pass=%d host_load1=%s capture_rows_total=%d\n",
		rounds, winnersScaleScopeSize, winnersScaleScopeSize/100, hostLoad(), capturedRows)
	var onPass, offPass, onCap []float64
	for i := 0; i < rounds; i++ {
		first := "capture-first"
		if (i+1)%2 == 0 {
			first = "control-first"
		}
		fmt.Fprintf(&b, "  round %d (%s): pass_with_capture=%s pass_without=%s capture_statement=%s\n",
			i+1, first, on[i].pass, off[i].pass, on[i].capture)
		onPass = append(onPass, float64(on[i].pass.Microseconds()))
		offPass = append(offPass, float64(off[i].pass.Microseconds()))
		onCap = append(onCap, float64(on[i].capture.Microseconds()))
	}
	mOn, mOff, mCap := median(onPass), median(offPass), median(onCap)
	fmt.Fprintf(&b, "  median pass_with_capture=%.1fms pass_without=%.1fms delta=%+.1f%% | median capture_statement=%.2fms = %.2f%% of the capture pass",
		mOn/1000, mOff/1000, 100*(mOn-mOff)/mOff, mCap/1000, 100*mCap/mOn)
	return b.String()
}

func median(xs []float64) float64 {
	c := append([]float64(nil), xs...)
	sort.Float64s(c)
	return c[len(c)/2]
}
