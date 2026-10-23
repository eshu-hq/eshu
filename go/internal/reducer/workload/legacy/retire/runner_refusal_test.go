// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retire

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	legacyworkload "github.com/eshu-hq/eshu/go/internal/graph/legacy/workload"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestMissingWriterRefusesExecuteWithItsOwnError pins that a Neo4j runner
// with no graph writer refuses execute as a wiring fault, not as an
// unvalidated backend, and still serves dry runs.
func TestMissingWriterRefusesExecuteWithItsOwnError(t *testing.T) {
	g := &stubGraph{}
	g.addEligible("a")
	audit := &recordingAudit{}
	r := newRunner(g, nil)
	r.Writer = nil
	r.Audit = audit

	_, err := r.Run(context.Background(), legacyworkload.RetireRequest{Reason: "r"})
	if !errors.Is(err, legacyworkload.ErrWriterUnavailable) {
		t.Fatalf("execute err = %v, want ErrWriterUnavailable", err)
	}
	if errors.Is(err, legacyworkload.ErrBackendUnvalidated) {
		t.Fatalf("execute err = %v, must not claim the backend is unvalidated", err)
	}
	if len(audit.events) != 1 || audit.events[0].ReasonCode != reasonRefusedWriterUnavailable ||
		audit.events[0].Decision != governanceaudit.DecisionDenied {
		t.Fatalf("audit = %+v, want one denied %s event", audit.events, reasonRefusedWriterUnavailable)
	}
	if _, err := governanceaudit.NormalizeEvent(audit.events[0]); err != nil {
		t.Fatalf("audit event is not valid: %v", err)
	}
	if g.reads != 0 {
		t.Fatalf("a refused execute must not read the graph; reads = %d", g.reads)
	}

	resp, err := r.Run(context.Background(), legacyworkload.RetireRequest{DryRun: true})
	if err != nil {
		t.Fatalf("dry run without a writer: %v", err)
	}
	if resp.Eligible != 1 {
		t.Fatalf("dry run eligible = %d, want 1", resp.Eligible)
	}
}

// TestExecuteCountsAndWarnsEveryJudgedNodeBeyondTheListCap pins that the
// outcome counter and the per-node WARN cover every node the request judged,
// not only the blocked nodes the response lists (maxBlockedListed).
func TestExecuteCountsAndWarnsEveryJudgedNodeBeyondTheListCap(t *testing.T) {
	reader := metric.NewManualReader()
	inst, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	const blocked = maxBlockedListed + 50
	g := &stubGraph{}
	g.addEligible("a")
	for i := range blocked {
		g.addNoTwin(fmt.Sprintf("b%03d", i))
	}
	var logs bytes.Buffer
	r := newRunner(g, &recordingWriter{deleted: 1})
	r.Instruments = inst
	r.Logger = slog.New(slog.NewTextHandler(&logs, nil))

	resp, err := r.Run(context.Background(), legacyworkload.RetireRequest{Reason: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Blocked != maxBlockedListed || !resp.Truncated {
		t.Fatalf("response blocked = %d truncated = %v, want the list capped at %d", resp.Blocked, resp.Truncated, maxBlockedListed)
	}
	got := collectOutcomes(t, reader)
	if got[OutcomeBlockedNoTwin] != blocked || got[OutcomeDeleted] != 1 {
		t.Fatalf("counter = %v, want %s=%d and %s=1", got, OutcomeBlockedNoTwin, blocked, OutcomeDeleted)
	}
	if n := strings.Count(logs.String(), "legacy workload kept"); n != blocked {
		t.Fatalf("WARN lines = %d, want one per kept node (%d)", n, blocked)
	}
}

func collectOutcomes(t *testing.T, reader *metric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_workload_legacy_retirements_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				outcome, _ := dp.Attributes.Value("outcome")
				got[outcome.AsString()] += dp.Value
			}
		}
	}
	return got
}
