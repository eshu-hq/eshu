package cypher

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// resolvedIDSet builds an authoritative resolved_id set from variadic ids.
func resolvedIDSet(ids ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

// TestClassifyReconciliationDriftInSync proves the fixture-intent ->
// reducer-graph-truth -> Postgres-truth agreement case: when the graph edge's
// denormalized generation and resolved_id match the authoritative Postgres
// generation, the edge is in sync and the pass reports convergence.
func TestClassifyReconciliationDriftInSync(t *testing.T) {
	t.Parallel()

	authoritative := []AuthoritativePostgresGeneration{{
		AcceptanceUnitID: "repo-a",
		GenerationID:     "gen-2",
		ResolvedIDs:      resolvedIDSet("resolved-1"),
	}}
	edges := []GraphDenormalizedEdge{{
		EdgeKey:          "edge-1",
		Domain:           "repo_dependency",
		AcceptanceUnitID: "repo-a",
		GenerationID:     "gen-2",
		ResolvedID:       "resolved-1",
	}}

	report := ClassifyReconciliationDrift(authoritative, edges)

	if !report.Converged() {
		t.Fatalf("expected converged report, got counts %v", report.Counts)
	}
	if got := report.Counts[ReconciliationDriftInSync]; got != 1 {
		t.Fatalf("in_sync count = %d, want 1", got)
	}
	if keys := report.DriftedEdgeKeys(); len(keys) != 0 {
		t.Fatalf("expected no drifted edges, got %v", keys)
	}
	if report.Findings[0].NeedsRetract() {
		t.Fatal("in-sync finding must not need retract")
	}
}

// TestClassifyReconciliationDriftStaleGenerationGraphBehind proves the
// partial-failure injection where Postgres swapped to a new authoritative
// generation but the graph still holds an edge stamped with the superseded
// generation (Postgres-ok / graph-retract-failed). The edge must be classified
// stale_generation and scheduled for retract so the graph converges.
func TestClassifyReconciliationDriftStaleGenerationGraphBehind(t *testing.T) {
	t.Parallel()

	authoritative := []AuthoritativePostgresGeneration{{
		AcceptanceUnitID: "repo-a",
		GenerationID:     "gen-2",
		ResolvedIDs:      resolvedIDSet("resolved-2"),
	}}
	edges := []GraphDenormalizedEdge{{
		EdgeKey:          "edge-old",
		Domain:           "repo_dependency",
		AcceptanceUnitID: "repo-a",
		GenerationID:     "gen-1", // superseded generation still on the graph
		ResolvedID:       "resolved-1",
	}}

	report := ClassifyReconciliationDrift(authoritative, edges)

	if report.Converged() {
		t.Fatal("expected drift, report reported convergence")
	}
	if got := report.Counts[ReconciliationDriftStaleGeneration]; got != 1 {
		t.Fatalf("stale_generation count = %d, want 1", got)
	}
	keys := report.DriftedEdgeKeys()
	if got := keys["repo_dependency"]; len(got) != 1 || got[0] != "edge-old" {
		t.Fatalf("drifted edges = %v, want [edge-old]", got)
	}
}

// TestClassifyReconciliationDriftStaleGenerationGraphAhead proves the inverse
// partial-failure: a graph write committed against a generation Postgres never
// made authoritative (graph-ok / Postgres-swap-failed-or-rolled-back). The edge
// generation is not the authoritative one, so it is stale and must be retracted
// to avoid stranding an inconsistent denormalized edge.
func TestClassifyReconciliationDriftStaleGenerationGraphAhead(t *testing.T) {
	t.Parallel()

	authoritative := []AuthoritativePostgresGeneration{{
		AcceptanceUnitID: "repo-a",
		GenerationID:     "gen-1", // Postgres never advanced past gen-1
		ResolvedIDs:      resolvedIDSet("resolved-1"),
	}}
	edges := []GraphDenormalizedEdge{{
		EdgeKey:          "edge-ahead",
		Domain:           "deployable_unit_edges",
		AcceptanceUnitID: "repo-a",
		GenerationID:     "gen-2", // graph wrote ahead of Postgres truth
		ResolvedID:       "resolved-9",
	}}

	report := ClassifyReconciliationDrift(authoritative, edges)

	if got := report.Counts[ReconciliationDriftStaleGeneration]; got != 1 {
		t.Fatalf("stale_generation count = %d, want 1", got)
	}
	if report.Converged() {
		t.Fatal("graph-ahead edge must not be reported as converged")
	}
}

// TestClassifyReconciliationDriftOrphanResolvedID proves the within-generation
// partial failure: the edge generation matches the authoritative generation but
// its resolved_id was retired from Postgres and never retracted from the graph.
func TestClassifyReconciliationDriftOrphanResolvedID(t *testing.T) {
	t.Parallel()

	authoritative := []AuthoritativePostgresGeneration{{
		AcceptanceUnitID: "repo-a",
		GenerationID:     "gen-2",
		ResolvedIDs:      resolvedIDSet("resolved-2"), // resolved-1 retired
	}}
	edges := []GraphDenormalizedEdge{{
		EdgeKey:          "edge-orphan",
		Domain:           "repo_dependency",
		AcceptanceUnitID: "repo-a",
		GenerationID:     "gen-2",
		ResolvedID:       "resolved-1", // no longer in the authoritative generation
	}}

	report := ClassifyReconciliationDrift(authoritative, edges)

	if got := report.Counts[ReconciliationDriftOrphanResolvedID]; got != 1 {
		t.Fatalf("orphan_resolved_id count = %d, want 1", got)
	}
	if !report.Findings[0].NeedsRetract() {
		t.Fatal("orphan finding must need retract")
	}
}

// TestClassifyReconciliationDriftRetiredAcceptanceUnit proves an edge whose
// acceptance unit Postgres no longer recognizes at all is stranded and retracted
// rather than silently kept.
func TestClassifyReconciliationDriftRetiredAcceptanceUnit(t *testing.T) {
	t.Parallel()

	edges := []GraphDenormalizedEdge{{
		EdgeKey:          "edge-ghost",
		Domain:           "repo_dependency",
		AcceptanceUnitID: "repo-gone",
		GenerationID:     "gen-1",
		ResolvedID:       "resolved-1",
	}}

	report := ClassifyReconciliationDrift(nil, edges)

	if got := report.Counts[ReconciliationDriftStaleGeneration]; got != 1 {
		t.Fatalf("stale_generation count = %d, want 1", got)
	}
	if report.Converged() {
		t.Fatal("edge for retired unit must not be reported converged")
	}
}

// TestClassifyReconciliationDriftResolvedIDOptional proves domains that do not
// carry a resolved_id reconcile on generation identity alone and stay in sync
// when their generation matches.
func TestClassifyReconciliationDriftResolvedIDOptional(t *testing.T) {
	t.Parallel()

	authoritative := []AuthoritativePostgresGeneration{{
		AcceptanceUnitID: "repo-a",
		GenerationID:     "gen-2",
		ResolvedIDs:      nil,
	}}
	edges := []GraphDenormalizedEdge{{
		EdgeKey:          "edge-platform",
		Domain:           "platform_infra",
		AcceptanceUnitID: "repo-a",
		GenerationID:     "gen-2",
		ResolvedID:       "", // domain carries no resolved_id
	}}

	report := ClassifyReconciliationDrift(authoritative, edges)

	if !report.Converged() {
		t.Fatalf("resolved_id-less edge should be in sync, counts %v", report.Counts)
	}
}

// TestRecordReconciliationConvergenceRecordsDriftKinds proves the operator-facing
// convergence counter records every non-zero drift class with a bounded
// drift_kind label, giving an operator a queryable signal that a reconciliation
// pass detected and is converging drift.
func TestRecordReconciliationConvergenceRecordsDriftKinds(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	report := ReconciliationReport{
		Counts: map[ReconciliationDriftClass]int{
			ReconciliationDriftInSync:           5,
			ReconciliationDriftStaleGeneration:  2,
			ReconciliationDriftOrphanResolvedID: 1,
		},
	}
	RecordReconciliationConvergence(context.Background(), instruments, report)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	const metricName = "eshu_dp_reconciliation_convergence_total"
	if got := metricCounterValue(t, rm, metricName, "drift_kind", "in_sync"); got != 5 {
		t.Fatalf("in_sync convergence = %d, want 5", got)
	}
	if got := metricCounterValue(t, rm, metricName, "drift_kind", "stale_generation"); got != 2 {
		t.Fatalf("stale_generation convergence = %d, want 2", got)
	}
	if got := metricCounterValue(t, rm, metricName, "drift_kind", "orphan_resolved_id"); got != 1 {
		t.Fatalf("orphan_resolved_id convergence = %d, want 1", got)
	}
}

// TestRecordReconciliationConvergenceNilInstrumentsSafe proves recording is a
// no-op when telemetry is unconfigured, so the reconciliation pass never panics
// in a bootstrap or test path without instruments.
func TestRecordReconciliationConvergenceNilInstrumentsSafe(t *testing.T) {
	t.Parallel()

	RecordReconciliationConvergence(context.Background(), nil, ReconciliationReport{
		Counts: map[ReconciliationDriftClass]int{ReconciliationDriftStaleGeneration: 1},
	})
}

// TestLogReconciliationReportWarnsOnDrift proves the operator log line escalates
// to warn and names the drifted edges when the graph has not converged, so a
// stranded denormalized edge surfaces in alert pipelines.
func TestLogReconciliationReportWarnsOnDrift(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	report := ClassifyReconciliationDrift(
		[]AuthoritativePostgresGeneration{{
			AcceptanceUnitID: "repo-a",
			GenerationID:     "gen-2",
			ResolvedIDs:      resolvedIDSet("resolved-2"),
		}},
		[]GraphDenormalizedEdge{{
			EdgeKey:          "edge-old",
			Domain:           "repo_dependency",
			AcceptanceUnitID: "repo-a",
			GenerationID:     "gen-1",
			ResolvedID:       "resolved-1",
		}},
	)
	LogReconciliationReport(logger, report)

	out := buf.String()
	if !strings.Contains(out, `"level":"WARN"`) {
		t.Fatalf("expected WARN level on drift, got %s", out)
	}
	if !strings.Contains(out, `"converged":false`) {
		t.Fatalf("expected converged=false, got %s", out)
	}
	if !strings.Contains(out, "edge-old") {
		t.Fatalf("expected drifted edge key in log, got %s", out)
	}
	if !strings.Contains(out, `"stale_generation":1`) {
		t.Fatalf("expected stale_generation count in log, got %s", out)
	}
}

// TestLogReconciliationReportInfoOnConvergence proves a converged pass logs at
// info, giving an operator positive confirmation the pass ran and found no
// stranded edges rather than inferring health from metric absence.
func TestLogReconciliationReportInfoOnConvergence(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	report := ClassifyReconciliationDrift(
		[]AuthoritativePostgresGeneration{{
			AcceptanceUnitID: "repo-a",
			GenerationID:     "gen-2",
			ResolvedIDs:      resolvedIDSet("resolved-2"),
		}},
		[]GraphDenormalizedEdge{{
			EdgeKey:          "edge-ok",
			Domain:           "repo_dependency",
			AcceptanceUnitID: "repo-a",
			GenerationID:     "gen-2",
			ResolvedID:       "resolved-2",
		}},
	)
	LogReconciliationReport(logger, report)

	out := buf.String()
	if !strings.Contains(out, `"level":"INFO"`) {
		t.Fatalf("expected INFO level on convergence, got %s", out)
	}
	if !strings.Contains(out, `"converged":true`) {
		t.Fatalf("expected converged=true, got %s", out)
	}
}

// TestLogReconciliationReportNilLoggerSafe proves logging is a no-op without a
// logger configured.
func TestLogReconciliationReportNilLoggerSafe(t *testing.T) {
	t.Parallel()

	LogReconciliationReport(nil, ReconciliationReport{
		Counts: map[ReconciliationDriftClass]int{ReconciliationDriftStaleGeneration: 1},
	})
}
