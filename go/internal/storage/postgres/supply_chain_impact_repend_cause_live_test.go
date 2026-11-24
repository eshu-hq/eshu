// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// passOutcomeKey is one eshu_dp_supply_chain_impact_pass_outcome_total series
// by the two labels the re-pend attribution reads.
type passOutcomeKey struct{ cause, changed string }

func passOutcomes(t *testing.T, reader *sdkmetric.ManualReader) map[passOutcomeKey]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	out := map[passOutcomeKey]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_pass_outcome_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				cause, _ := dp.Attributes.Value(telemetry.MetricDimensionRependCause)
				changed, _ := dp.Attributes.Value(telemetry.MetricDimensionPassChanged)
				out[passOutcomeKey{cause.AsString(), changed.AsString()}] += dp.Value
			}
		}
	}
	return out
}

// outcomeDelta runs step and returns the pass-outcome points it added.
func outcomeDelta(t *testing.T, reader *sdkmetric.ManualReader, step func()) map[passOutcomeKey]int64 {
	t.Helper()
	before := passOutcomes(t, reader)
	step()
	after := passOutcomes(t, reader)
	delta := map[passOutcomeKey]int64{}
	for key, value := range after {
		if d := value - before[key]; d != 0 {
			delta[key] = d
		}
	}
	return delta
}

func wantOneOutcome(t *testing.T, step string, delta map[passOutcomeKey]int64, want passOutcomeKey) {
	t.Helper()
	if len(delta) != 1 || delta[want] != 1 {
		t.Fatalf("%s: pass outcomes %v, want exactly one %+v", step, delta, want)
	}
}

// drainAll plays the worker until nothing is claimable, up to maxClaims, like
// convergence.drain with a bound sized for a fleet. It returns the claims run.
func (c *convergence) drainAll(maxClaims int) int {
	c.t.Helper()
	for i := 0; i < maxClaims; i++ {
		intent, ok, err := c.queue.Claim(c.ctx)
		if err != nil {
			c.t.Fatalf("claim: %v", err)
		}
		if !ok {
			return i
		}
		c.handled = append(c.handled, fmt.Sprintf("%s#%d", intent.ScopeID, intent.AttemptCount))
		result, err := c.handler.Handle(c.ctx, intent)
		if err != nil {
			c.t.Fatalf("handle %s: %v", intent.ScopeID, err)
		}
		if err := c.queue.Ack(c.ctx, intent, result); err != nil {
			c.t.Fatalf("ack %s: %v", intent.ScopeID, err)
		}
	}
	c.t.Fatalf("queue still had work after %d claims", maxClaims)
	return maxClaims
}

// claimHandleAck runs one claimed pass to its acknowledgement and returns its
// result.
func (c *convergence) claimHandleAck(scope string) reducercontract.Result {
	c.t.Helper()
	intent := c.claimTarget(scope)
	result, err := c.handler.Handle(c.ctx, intent)
	if err != nil {
		c.t.Fatalf("pass %s: %v", scope, err)
	}
	if err := c.queue.Ack(c.ctx, intent, result); err != nil {
		c.t.Fatal(err)
	}
	return result
}

// TestRependCauseLabelsFollowTheRealQueueLive is the end-to-end proof of the
// #11339 re-pend attribution (#10972 reality audit, risk 10): every pass runs
// through ReducerQueue.Claim, the real handler and Ack, and the
// eshu_dp_supply_chain_impact_pass_outcome_total point each pass writes must
// carry the cause the queue really produced. The unit test
// (repend_cause_test.go) builds intents by hand; this one takes them from the
// queue, so a claim or trigger change that alters EnqueuedAt, CycleStartedAt,
// the attempt count or the payload shows up here.
//
// Steps and the label each must write: a brand-new scope's first pass is
// first/changed; a succeeded holder reopened by the source's fan-out is
// fanout_rerun/changed; a retryable failure's next attempt is retry; a
// continuation claim is drain/unchanged.
//
// Expected on main: GREEN.
func TestRependCauseLabelsFollowTheRealQueueLive(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	c := newConvergence(t, false)
	c.handler.Instruments = inst
	c.drain()

	const fresh = "scope:10972:cause:first"
	c.seedNoRowHolder(fresh)
	fanOutSeedItem(t, c.ctx, c.db, fresh, "g1")
	wantOneOutcome(t, "first pass", outcomeDelta(t, reader, func() { c.claimHandleAck(fresh) }),
		passOutcomeKey{telemetry.SupplyChainImpactRependCauseFirst, telemetry.SupplyChainImpactPassChanged})
	c.drainAll(50) // the fresh scope's own fan-out reopened the other holders

	// The source's change reopens the succeeded EPSS holder with a stamp.
	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)
	c.claimHandleAck(convergenceSource)
	// Both copy holders (the EPSS scope and the fresh scope) were reopened
	// with the stamp; each rerun is a one-hop fanout_rerun that changed its copy.
	delta := outcomeDelta(t, reader, func() { c.drainAll(50) })
	if len(delta) != 1 || delta[passOutcomeKey{telemetry.SupplyChainImpactRependCauseFanOutRerun, telemetry.SupplyChainImpactPassChanged}] != 2 {
		t.Fatalf("fan-out reruns: pass outcomes %v, want exactly two fanout_rerun/changed", delta)
	}

	// A retryable failure: the next attempt is a retry.
	c.setSourceStatus("Analyzed")
	c.rerun(convergenceSource)
	failed := c.claimTarget(convergenceSource)
	if err := c.queue.Fail(c.ctx, failed, raceRetryableErr{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_work_items SET visible_at = now() - interval '1 second', next_attempt_at = NULL WHERE work_item_id = $1`, failed.IntentID); err != nil {
		t.Fatal(err)
	}
	delta = outcomeDelta(t, reader, func() { c.claimHandleAck(convergenceSource) })
	if len(delta) != 1 || delta[passOutcomeKey{telemetry.SupplyChainImpactRependCauseRetry, telemetry.SupplyChainImpactPassChanged}] != 1 {
		t.Fatalf("retry: pass outcomes %v, want exactly one retry/changed", delta)
	}
	c.drainAll(50)

	// A continuation claim: the stored list makes it a drain, and an unchanged
	// source reports unchanged.
	list, _ := json.Marshal([]string{convergenceHolder})
	c.rerun(convergenceSource)
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_work_items
SET payload = payload || jsonb_build_object('fanout_remaining', $2::jsonb, 'fanout_owed', true)
WHERE scope_id = $1 AND domain = 'supply_chain_impact' AND status = 'pending'`, convergenceSource, string(list)); err != nil {
		t.Fatal(err)
	}
	wantOneOutcome(t, "drain", outcomeDelta(t, reader, func() { c.claimHandleAck(convergenceSource) }),
		passOutcomeKey{telemetry.SupplyChainImpactRependCauseDrain, telemetry.SupplyChainImpactPassUnchanged})
}
