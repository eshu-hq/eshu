// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// #8472 (pacing half): the per-domain depth observation must feed the pacing
// signals end to end. These proofs chain the real observer output shape
// (workflow.ClaimPacingDepths: one family's pending depth plus the global
// ceiling) through the real verdict (workflow.DecideClaimPacing) into the
// real latch advance and emission (advanceClaimPacingLatch against a fake
// database whose latch writes always change a row, then recordClaimPacing),
// and assert what the instruments record.
// quota_exceeded is excluded: it lives in unmerged PR #8557.

const (
	pacingSignalsEngagedMetric = "eshu_dp_workflow_claim_pacing_engaged_total"
	pacingSignalsActiveMetric  = "eshu_dp_workflow_claim_pacing_active"
	pacingSignalsStuckMetric   = "eshu_dp_workflow_claim_pacing_stuck_total"
	pacingSignalsDelayMetric   = "eshu_dp_workflow_claim_pacing_delay_seconds"
)

func pacingSignalsTestConfig() workflow.ClaimPacingConfig {
	return workflow.ClaimPacingConfig{
		DomainHigh:    10,
		DomainLow:     4,
		GlobalHigh:    100,
		GlobalLow:     40,
		GrantDelay:    2 * time.Second,
		MaxEngagement: time.Minute,
	}
}

func newPacingSignalsStore(t *testing.T) (*WorkflowControlStore, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	return &WorkflowControlStore{Instruments: inst, database: &sourceBackpressureFakeDB{}}, reader
}

// pacingSignalPoint is one collected data point: its value (counter sum or
// histogram sum), histogram count, and domain label ("<none>" when the
// global ceiling carries no domain label).
type pacingSignalPoint struct {
	value  float64
	count  uint64
	domain string
}

func pacingSignalPoints(t *testing.T, reader *sdkmetric.ManualReader, name string) map[string]pacingSignalPoint {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	points := map[string]pacingSignalPoint{}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name != name {
				continue
			}
			record := func(attrs attribute.Set, value float64, count uint64) {
				signal, _ := attrs.Value(attribute.Key("pacing_signal"))
				domain, ok := attrs.Value(attribute.Key("domain"))
				key := signal.AsString()
				domainStr := "<none>"
				if ok {
					domainStr = domain.AsString()
				}
				points[key] = pacingSignalPoint{value: value, count: count, domain: domainStr}
			}
			switch data := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, p := range data.DataPoints {
					record(p.Attributes, float64(p.Value), 0)
				}
			case metricdata.Sum[float64]:
				for _, p := range data.DataPoints {
					record(p.Attributes, p.Value, 0)
				}
			case metricdata.Histogram[float64]:
				for _, p := range data.DataPoints {
					record(p.Attributes, p.Sum, p.Count)
				}
			default:
				t.Fatalf("%s data = %T, want Sum or Histogram", name, m.Data)
			}
		}
	}
	return points
}

// decideAndRecord mirrors CheckClaimPacing's verdict-to-emission path without
// storage: the depth observation feeds DecideClaimPacing, the store names the
// saturated family, and recordClaimPacing emits the signals.
func decideAndRecord(
	cfg workflow.ClaimPacingConfig,
	depths workflow.ClaimPacingDepths,
	latch workflow.ClaimPacingLatch,
	engagedFor time.Duration,
	store *WorkflowControlStore,
	domainKey string,
) workflow.ClaimPacingDecision {
	decision := workflow.DecideClaimPacing(cfg, depths, latch, engagedFor)
	decision.Domain = pacingDecisionDomain(decision.Signal, domainKey)
	prior := claimPacingLatchState{rows: map[workflow.ClaimPacingSignal]claimPacingLatchRow{}}
	if latch.Engaged {
		prior.rows[latch.Signal] = claimPacingLatchRow{engaged: true}
	}
	if err := store.advanceClaimPacingLatch(context.Background(), domainKey, prior, decision, time.Now()); err != nil {
		panic(err)
	}
	store.recordClaimPacing(context.Background(), decision)
	return decision
}

func TestClaimPacingSignalsDomainDepthEngagement(t *testing.T) {
	cfg := pacingSignalsTestConfig()
	store, reader := newPacingSignalsStore(t)
	const domainKey = "git/test-instance"

	// Only the claiming family's domain depth is hot; the global ceiling is
	// cold. The engagement must land on the domain signal naming that
	// family, not on the global aggregate.
	decision := decideAndRecord(cfg,
		workflow.ClaimPacingDepths{Domain: 50, Global: 50},
		workflow.ClaimPacingLatch{},
		0, store, domainKey)
	if !decision.Paced {
		t.Fatalf("DecideClaimPacing() paced = false, want true for hot domain depth")
	}
	if decision.Signal != workflow.ClaimPacingSignalDomain {
		t.Fatalf("DecideClaimPacing() signal = %q, want %q", decision.Signal, workflow.ClaimPacingSignalDomain)
	}

	engaged := pacingSignalPoints(t, reader, pacingSignalsEngagedMetric)
	got, ok := engaged["domain"]
	if !ok {
		t.Fatalf("engaged_total has no domain-signal point, want one engagement")
	}
	if got.value != 1 {
		t.Fatalf("engaged_total{domain} = %v, want 1", got.value)
	}
	if got.domain != domainKey {
		t.Fatalf("engaged_total{domain} domain = %q, want %q", got.domain, domainKey)
	}

	active := pacingSignalPoints(t, reader, pacingSignalsActiveMetric)
	if got, ok := active["domain"]; !ok || got.value != 1 {
		t.Fatalf("active{domain} = %+v, want +1 on engagement", active)
	}

	delay := pacingSignalPoints(t, reader, pacingSignalsDelayMetric)
	dpoint, ok := delay["domain"]
	if !ok || dpoint.count != 1 {
		t.Fatalf("delay_seconds{domain} = %+v, want one recorded grant delay", delay)
	}
	if dpoint.value != cfg.GrantDelay.Seconds() {
		t.Fatalf("delay_seconds{domain} sum = %v, want GrantDelay %v", dpoint.value, cfg.GrantDelay.Seconds())
	}
	if dpoint.domain != domainKey {
		t.Fatalf("delay_seconds{domain} domain = %q, want %q", dpoint.domain, domainKey)
	}

	if stuck := pacingSignalPoints(t, reader, pacingSignalsStuckMetric); len(stuck) != 0 {
		t.Fatalf("stuck_total = %+v, want no points on a fresh engagement", stuck)
	}
}

func TestClaimPacingSignalsColdDomainStaysSilent(t *testing.T) {
	cfg := pacingSignalsTestConfig()
	store, reader := newPacingSignalsStore(t)

	decision := decideAndRecord(cfg,
		workflow.ClaimPacingDepths{Domain: 2, Global: 50},
		workflow.ClaimPacingLatch{},
		0, store, "git/quiet-instance")
	if decision.Paced {
		t.Fatalf("DecideClaimPacing() paced = true, want false for cold depths")
	}
	for _, name := range []string{
		pacingSignalsEngagedMetric, pacingSignalsActiveMetric,
		pacingSignalsStuckMetric, pacingSignalsDelayMetric,
	} {
		if points := pacingSignalPoints(t, reader, name); len(points) != 0 {
			t.Fatalf("%s = %+v, want no points for an unpaced verdict", name, points)
		}
	}
}

func TestClaimPacingSignalsReleaseDropsActiveGauge(t *testing.T) {
	cfg := pacingSignalsTestConfig()
	store, reader := newPacingSignalsStore(t)
	const domainKey = "git/test-instance"
	prior := workflow.ClaimPacingLatch{Engaged: true, Signal: workflow.ClaimPacingSignalDomain}

	// This process engages, then depth drains to the release watermark: the
	// verdict goes unpaced and the gauge takes back the raise this process
	// made, with no second engagement counted.
	decideAndRecord(cfg, workflow.ClaimPacingDepths{Domain: 50, Global: 50},
		workflow.ClaimPacingLatch{}, 0, store, domainKey)
	decision := decideAndRecord(cfg,
		workflow.ClaimPacingDepths{Domain: 4, Global: 10},
		prior, 0, store, domainKey)
	if decision.Paced {
		t.Fatalf("DecideClaimPacing() paced = true, want false at the release watermark")
	}
	if engaged := pacingSignalPoints(t, reader, pacingSignalsEngagedMetric); engaged["domain"].value != 1 {
		t.Fatalf("engaged_total = %+v, want the one engagement, none added by the release", engaged)
	}
	active := pacingSignalPoints(t, reader, pacingSignalsActiveMetric)
	got, ok := active["domain"]
	if !ok || got.value != 0 {
		t.Fatalf("active{domain} = %+v, want 0 after this process's engage and release", active)
	}
	if got.domain != domainKey {
		t.Fatalf("active{domain} domain = %q, want %q", got.domain, domainKey)
	}
}

// TestClaimPacingSignalsReleaseWithoutRaiseLeavesGaugeAlone pins the #10882
// contract: a process that never raised the gauge (it observed an engagement
// another process made) must not push it below zero when it writes the
// release. The process that did raise it drops it on its next observation.
func TestClaimPacingSignalsReleaseWithoutRaiseLeavesGaugeAlone(t *testing.T) {
	cfg := pacingSignalsTestConfig()
	store, reader := newPacingSignalsStore(t)
	const domainKey = "git/test-instance"
	prior := workflow.ClaimPacingLatch{Engaged: true, Signal: workflow.ClaimPacingSignalDomain}

	decideAndRecord(cfg, workflow.ClaimPacingDepths{Domain: 4, Global: 10}, prior, 0, store, domainKey)

	if active := pacingSignalPoints(t, reader, pacingSignalsActiveMetric); len(active) != 0 {
		t.Fatalf("active = %+v, want no point: this process never raised the gauge", active)
	}
	if engaged := pacingSignalPoints(t, reader, pacingSignalsEngagedMetric); len(engaged) != 0 {
		t.Fatalf("engaged_total = %+v, want no engagement on a release", engaged)
	}
}

func TestClaimPacingSignalsSteadyEngagementEmitsDelayOnly(t *testing.T) {
	cfg := pacingSignalsTestConfig()
	store, reader := newPacingSignalsStore(t)
	const domainKey = "git/test-instance"
	prior := workflow.ClaimPacingLatch{Engaged: true, Signal: workflow.ClaimPacingSignalDomain}

	// Still hot inside the engagement bound: paced but not stuck. No latch
	// transition means no counter or gauge deltas, yet every paced grant
	// still carries its delay into the histogram.
	decision := decideAndRecord(cfg,
		workflow.ClaimPacingDepths{Domain: 50, Global: 50},
		prior, 30*time.Second, store, domainKey)
	if !decision.Paced || decision.Stuck {
		t.Fatalf("DecideClaimPacing() = %+v, want paced and not stuck", decision)
	}
	if engaged := pacingSignalPoints(t, reader, pacingSignalsEngagedMetric); len(engaged) != 0 {
		t.Fatalf("engaged_total = %+v, want no delta without a transition", engaged)
	}
	if active := pacingSignalPoints(t, reader, pacingSignalsActiveMetric); len(active) != 0 {
		t.Fatalf("active = %+v, want no delta without a transition", active)
	}
	if stuck := pacingSignalPoints(t, reader, pacingSignalsStuckMetric); len(stuck) != 0 {
		t.Fatalf("stuck_total = %+v, want no points inside the bound", stuck)
	}
	delay := pacingSignalPoints(t, reader, pacingSignalsDelayMetric)
	if got, ok := delay["domain"]; !ok || got.count != 1 {
		t.Fatalf("delay_seconds{domain} = %+v, want one recorded grant delay", delay)
	}
}

func TestClaimPacingSignalsStuckEngagement(t *testing.T) {
	cfg := pacingSignalsTestConfig()
	store, reader := newPacingSignalsStore(t)
	const domainKey = "git/test-instance"
	prior := workflow.ClaimPacingLatch{Engaged: true, Signal: workflow.ClaimPacingSignalDomain}

	// Past the engagement bound with depth still above release: still
	// paced, and the stuck counter fires so the wedged throttle stays loud.
	decision := decideAndRecord(cfg,
		workflow.ClaimPacingDepths{Domain: 50, Global: 50},
		prior, 2*time.Minute, store, domainKey)
	if !decision.Paced || !decision.Stuck {
		t.Fatalf("DecideClaimPacing() = %+v, want paced and stuck", decision)
	}
	stuck := pacingSignalPoints(t, reader, pacingSignalsStuckMetric)
	got, ok := stuck["domain"]
	if !ok || got.value != 1 {
		t.Fatalf("stuck_total{domain} = %+v, want 1", stuck)
	}
	if got.domain != domainKey {
		t.Fatalf("stuck_total{domain} domain = %q, want %q", got.domain, domainKey)
	}
}

func TestClaimPacingSignalsHandoffDomainToGlobal(t *testing.T) {
	cfg := pacingSignalsTestConfig()
	store, reader := newPacingSignalsStore(t)
	const domainKey = "git/test-instance"
	prior := workflow.ClaimPacingLatch{Engaged: true, Signal: workflow.ClaimPacingSignalDomain}

	// A domain-engaged latch draining to the release watermark while the
	// global ceiling is hot must hand off: domain active drops, global
	// engages with no domain label, and the delay rides the global attrs.
	// A regression dropping either side of the handoff stays loud. This
	// process engaged the domain first, so the handoff takes its raise back.
	decideAndRecord(cfg, workflow.ClaimPacingDepths{Domain: 50, Global: 50},
		workflow.ClaimPacingLatch{}, 0, store, domainKey)
	decision := decideAndRecord(cfg,
		workflow.ClaimPacingDepths{Domain: 4, Global: 500},
		prior, 0, store, domainKey)
	if !decision.Paced || decision.Signal != workflow.ClaimPacingSignalGlobal {
		t.Fatalf("DecideClaimPacing() = %+v, want paced on the global signal", decision)
	}

	active := pacingSignalPoints(t, reader, pacingSignalsActiveMetric)
	if got, ok := active["domain"]; !ok || got.value != 0 {
		t.Fatalf("active{domain} = %+v, want 0 once the released domain side is taken back", active)
	}
	engaged := pacingSignalPoints(t, reader, pacingSignalsEngagedMetric)
	got, ok := engaged["global"]
	if !ok || got.value != 1 {
		t.Fatalf("engaged_total{global} = %+v, want 1", engaged)
	}
	if got.domain != "<none>" {
		t.Fatalf("engaged_total{global} carries domain = %q, want no domain label", got.domain)
	}
	if got, ok := active["global"]; !ok || got.value != 1 {
		t.Fatalf("active{global} = %+v, want +1", active)
	}
	delay := pacingSignalPoints(t, reader, pacingSignalsDelayMetric)
	if got, ok := delay["global"]; !ok || got.count != 1 || got.domain != "<none>" {
		t.Fatalf("delay_seconds{global} = %+v, want one delay with no domain label", delay)
	}
}

func TestClaimPacingSignalsGlobalCeilingCarriesNoDomain(t *testing.T) {
	cfg := pacingSignalsTestConfig()
	store, reader := newPacingSignalsStore(t)

	// The claiming family is quiet but the system-wide ceiling is hot: the
	// global signal engages with no domain label, so the series cannot
	// explode per family.
	decision := decideAndRecord(cfg,
		workflow.ClaimPacingDepths{Domain: 2, Global: 500},
		workflow.ClaimPacingLatch{},
		0, store, "git/quiet-instance")
	if !decision.Paced || decision.Signal != workflow.ClaimPacingSignalGlobal {
		t.Fatalf("DecideClaimPacing() = %+v, want paced on the global signal", decision)
	}
	if decision.Domain != "" {
		t.Fatalf("decision Domain = %q, want empty for the global ceiling", decision.Domain)
	}
	engaged := pacingSignalPoints(t, reader, pacingSignalsEngagedMetric)
	got, ok := engaged["global"]
	if !ok || got.value != 1 {
		t.Fatalf("engaged_total{global} = %+v, want 1", engaged)
	}
	if got.domain != "<none>" {
		t.Fatalf("engaged_total{global} carries domain = %q, want no domain label", got.domain)
	}
	delay := pacingSignalPoints(t, reader, pacingSignalsDelayMetric)
	if got, ok := delay["global"]; !ok || got.count != 1 || got.domain != "<none>" {
		t.Fatalf("delay_seconds{global} = %+v, want one delay with no domain label", delay)
	}
}
