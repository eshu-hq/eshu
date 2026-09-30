// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package writer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const repositoryStubsMetric = "eshu_dp_canonical_repository_stubs_created_total"

// stubCountingExecutor reports Bolt-style write counts for every statement it
// runs, as the production reducer Bolt seam does. nodesCreated maps a Cypher
// template to the NodesCreated the backend reports for it. groupAttempts > 1
// simulates a driver retry: every attempt re-reports each statement, and only
// the last attempt commits.
type stubCountingExecutor struct {
	nodesCreated  map[string]int64
	groupAttempts int
	silent        bool
	calls         []sourcecypher.Statement
}

func (e *stubCountingExecutor) report(ctx context.Context, stmt sourcecypher.Statement) {
	if e.silent {
		return
	}
	sourcecypher.ReportWriteCounts(ctx, stmt.Cypher, stmt.Parameters,
		sourcecypher.WriteCounters{NodesCreated: e.nodesCreated[stmt.Cypher]})
}

func (e *stubCountingExecutor) Execute(ctx context.Context, stmt sourcecypher.Statement) error {
	e.calls = append(e.calls, stmt)
	e.report(ctx, stmt)
	return nil
}

// groupStubCountingExecutor adds the atomic GroupExecutor shape.
type groupStubCountingExecutor struct{ *stubCountingExecutor }

func (e groupStubCountingExecutor) ExecuteGroup(ctx context.Context, stmts []sourcecypher.Statement) error {
	attempts := e.groupAttempts
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		for _, stmt := range stmts {
			e.report(ctx, stmt)
		}
	}
	e.calls = append(e.calls, stmts...)
	return nil
}

func stubRepoDependencyRow(target string) reducer.SharedProjectionIntentRow {
	return reducer.SharedProjectionIntentRow{
		IntentID:     "intent-" + target,
		RepositoryID: "repository:source",
		ScopeID:      "scope-source",
		GenerationID: "gen-7446",
		Payload: map[string]any{
			"repo_id":           "repository:source",
			"target_repo_id":    target,
			"relationship_type": "DEPENDS_ON",
			"generation_id":     "gen-7446",
			"confidence":        0.9,
			"evidence_count":    1,
		},
	}
}

func stubSubmodulePinRow(target string) reducer.SharedProjectionIntentRow {
	return reducer.SharedProjectionIntentRow{
		IntentID:     "intent-pin-" + target,
		RepositoryID: "repository:parent",
		Payload: map[string]any{
			"parent_repo_id":   "repository:parent",
			"resolved_repo_id": target,
			"submodule_path":   "vendor/lib",
			"generation_id":    "gen-7446",
		},
	}
}

func newStubMetrics(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return instruments, reader
}

// stubCounterTotal sums the stubs counter over points labeled writer=writerLabel.
func stubCounterTotal(t *testing.T, reader *sdkmetric.ManualReader, writerLabel string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != repositoryStubsMetric {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", repositoryStubsMetric, m.Data)
			}
			for _, point := range sum.DataPoints {
				if attributesMatch(point.Attributes, map[string]string{"writer": writerLabel}) {
					total += point.Value
				}
			}
		}
	}
	return total
}

func stubLogEntries(buf *bytes.Buffer, msg string) []map[string]any {
	var out []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var entry map[string]any
		if json.Unmarshal(line, &entry) == nil && entry["msg"] == msg {
			out = append(out, entry)
		}
	}
	return out
}

func TestWriteEdgesCountsRepositoryStubsPerStatementShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		domain        string
		rows          []reducer.SharedProjectionIntentRow
		cypher        string
		grouped       bool
		groupAttempts int
		created       int64
		writerLabel   string
		want          int64
	}{
		{
			name: "repo_dependency atomic group", domain: reducer.DomainRepoDependency,
			rows:   []reducer.SharedProjectionIntentRow{stubRepoDependencyRow("repository:retired")},
			cypher: sourcecypher.BatchCanonicalRepoDependencyUpsertCypher, grouped: true,
			created: 1, writerLabel: telemetry.RepositoryStubWriterRepoDependency, want: 1,
		},
		{
			name: "repo_dependency group retried counts the committed attempt once", domain: reducer.DomainRepoDependency,
			rows:   []reducer.SharedProjectionIntentRow{stubRepoDependencyRow("repository:retired")},
			cypher: sourcecypher.BatchCanonicalRepoDependencyUpsertCypher, grouped: true, groupAttempts: 3,
			created: 1, writerLabel: telemetry.RepositoryStubWriterRepoDependency, want: 1,
		},
		{
			name: "repo_dependency sequential", domain: reducer.DomainRepoDependency,
			rows:    []reducer.SharedProjectionIntentRow{stubRepoDependencyRow("repository:retired")},
			cypher:  sourcecypher.BatchCanonicalRepoDependencyUpsertCypher,
			created: 1, writerLabel: telemetry.RepositoryStubWriterRepoDependency, want: 1,
		},
		{
			name: "submodule_pin atomic group", domain: reducer.DomainSubmodulePinEdges,
			rows:   []reducer.SharedProjectionIntentRow{stubSubmodulePinRow("repository:retired")},
			cypher: sourcecypher.BatchCanonicalSubmodulePinEdgeCypher, grouped: true,
			created: 1, writerLabel: telemetry.RepositoryStubWriterSubmodulePin, want: 1,
		},
		{
			name: "submodule_pin sequential", domain: reducer.DomainSubmodulePinEdges,
			rows:    []reducer.SharedProjectionIntentRow{stubSubmodulePinRow("repository:retired")},
			cypher:  sourcecypher.BatchCanonicalSubmodulePinEdgeCypher,
			created: 1, writerLabel: telemetry.RepositoryStubWriterSubmodulePin, want: 1,
		},
		{
			name: "repo_dependency to a live target creates nothing", domain: reducer.DomainRepoDependency,
			rows:   []reducer.SharedProjectionIntentRow{stubRepoDependencyRow("repository:live")},
			cypher: sourcecypher.BatchCanonicalRepoDependencyUpsertCypher, grouped: true,
			created: 0, writerLabel: telemetry.RepositoryStubWriterRepoDependency, want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			instruments, reader := newStubMetrics(t)
			base := &stubCountingExecutor{nodesCreated: map[string]int64{tt.cypher: tt.created}, groupAttempts: tt.groupAttempts}
			var exec sourcecypher.Executor = base
			if tt.grouped {
				exec = groupStubCountingExecutor{base}
			}
			var logs bytes.Buffer
			writer := NewEdgeWriter(exec, 0)
			writer.Instruments = instruments
			writer.Logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			if _, err := writer.WriteEdges(context.Background(), tt.domain, tt.rows, "resolver/cross-repo"); err != nil {
				t.Fatalf("WriteEdges() error = %v", err)
			}
			if got := stubCounterTotal(t, reader, tt.writerLabel); got != tt.want {
				t.Fatalf("%s{writer=%s} = %d, want %d", repositoryStubsMetric, tt.writerLabel, got, tt.want)
			}
			created := stubLogEntries(&logs, "canonical repository stub created")
			if tt.want == 0 {
				if len(created) != 0 {
					t.Fatalf("stub-created log lines = %d, want 0: %v", len(created), created)
				}
				return
			}
			if len(created) != 1 {
				t.Fatalf("stub-created log lines = %d, want 1; logs:\n%s", len(created), logs.String())
			}
			entry := created[0]
			want := map[string]any{
				"level": "INFO", "writer": tt.writerLabel, "nodes_created": float64(tt.created),
				"target_repo_id": "repository:retired", "generation_id": "gen-7446", "attributed": true,
			}
			for key, value := range want {
				if entry[key] != value {
					t.Fatalf("stub-created log %s = %#v, want %#v; entry = %v", key, entry[key], value, entry)
				}
			}
			if entry["source_repo_id"] == nil || entry["source_repo_id"] == "" {
				t.Fatalf("stub-created log has no source_repo_id: %v", entry)
			}
		})
	}
}

// An executor that reports no write summary (a test executor, or differential
// capture on) must record nothing and say so at DEBUG, never claim a stub count.
func TestWriteEdgesRepositoryStubsNotCountedWithoutWriteSummary(t *testing.T) {
	t.Parallel()
	instruments, reader := newStubMetrics(t)
	base := &stubCountingExecutor{silent: true}
	var logs bytes.Buffer
	writer := NewEdgeWriter(groupStubCountingExecutor{base}, 0)
	writer.Instruments = instruments
	writer.Logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	rows := []reducer.SharedProjectionIntentRow{stubRepoDependencyRow("repository:retired")}
	if _, err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "resolver/cross-repo"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got := stubCounterTotal(t, reader, telemetry.RepositoryStubWriterRepoDependency); got != 0 {
		t.Fatalf("%s = %d, want 0 without a write summary", repositoryStubsMetric, got)
	}
	notCounted := stubLogEntries(&logs, "canonical repository stubs not counted: executor reported no write summary")
	if len(notCounted) != 1 || notCounted[0]["level"] != "DEBUG" {
		t.Fatalf("not-counted DEBUG lines = %v, want exactly one; logs:\n%s", notCounted, logs.String())
	}
}

// The stub collector must forward every entry to a collector the caller
// already stashed. (The #6783 differential recorder is not one: with capture
// on it wraps below the edge writer and stashes its own collector.)
func TestWriteEdgesRepositoryStubCaptureForwardsToOuterCollector(t *testing.T) {
	t.Parallel()
	outer := sourcecypher.NewWriteCountsCollector()
	ctx := sourcecypher.WithWriteCountsCollector(context.Background(), outer)
	base := &stubCountingExecutor{nodesCreated: map[string]int64{sourcecypher.BatchCanonicalRepoDependencyUpsertCypher: 1}}
	writer := NewEdgeWriter(groupStubCountingExecutor{base}, 0)
	rows := []reducer.SharedProjectionIntentRow{stubRepoDependencyRow("repository:retired")}
	if _, err := writer.WriteEdges(ctx, reducer.DomainRepoDependency, rows, "resolver/cross-repo"); err != nil {
		t.Fatalf("WriteEdges() error = %v", err)
	}
	if got := len(outer.Entries()); got != 1 {
		t.Fatalf("outer collector entries = %d, want 1 (forwarded)", got)
	}
}

// A batch with several intent rows is one statement, and the backend counts
// NodesCreated per statement, not per row. The INFO line must say so
// (attributed=false, batch_rows=n) and list at most repositoryStubCandidateLimit
// source->target@generation pairs instead of naming one target.
func TestWriteEdgesRepositoryStubMultiRowLogListsUnattributedCandidates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		rowCount       int
		wantCandidates int
	}{
		{name: "two rows list both candidates", rowCount: 2, wantCandidates: 2},
		{name: "more rows than the limit are capped", rowCount: repositoryStubCandidateLimit + 2, wantCandidates: repositoryStubCandidateLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			instruments, reader := newStubMetrics(t)
			created := int64(tt.rowCount)
			base := &stubCountingExecutor{nodesCreated: map[string]int64{sourcecypher.BatchCanonicalRepoDependencyUpsertCypher: created}}
			var logs bytes.Buffer
			writer := NewEdgeWriter(groupStubCountingExecutor{base}, 0)
			writer.Instruments = instruments
			writer.Logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			rows := make([]reducer.SharedProjectionIntentRow, 0, tt.rowCount)
			for i := 0; i < tt.rowCount; i++ {
				rows = append(rows, stubRepoDependencyRow(fmt.Sprintf("repository:retired-%02d", i)))
			}
			if _, err := writer.WriteEdges(context.Background(), reducer.DomainRepoDependency, rows, "resolver/cross-repo"); err != nil {
				t.Fatalf("WriteEdges() error = %v", err)
			}
			if got := stubCounterTotal(t, reader, telemetry.RepositoryStubWriterRepoDependency); got != created {
				t.Fatalf("%s{writer=repo_dependency} = %d, want %d", repositoryStubsMetric, got, created)
			}
			entries := stubLogEntries(&logs, "canonical repository stub created")
			if len(entries) != 1 {
				t.Fatalf("stub-created log lines = %d, want 1 for one statement; logs:\n%s", len(entries), logs.String())
			}
			entry := entries[0]
			if entry["attributed"] != false || entry["batch_rows"] != float64(tt.rowCount) || entry["nodes_created"] != float64(created) {
				t.Fatalf("multi-row log = %v, want attributed=false batch_rows=%d nodes_created=%d", entry, tt.rowCount, created)
			}
			for _, key := range []string{"source_repo_id", "target_repo_id"} {
				if _, present := entry[key]; present {
					t.Fatalf("multi-row log names %s although the backend counts per statement: %v", key, entry)
				}
			}
			candidates, _ := entry["candidates"].([]any)
			if len(candidates) != tt.wantCandidates {
				t.Fatalf("candidates = %d, want %d: %v", len(candidates), tt.wantCandidates, candidates)
			}
			seen := map[string]bool{}
			for _, candidate := range candidates {
				text, _ := candidate.(string)
				if !strings.HasPrefix(text, "repository:source->repository:retired-") || !strings.HasSuffix(text, "@gen-7446") || seen[text] {
					t.Fatalf("candidate %q is malformed or repeated: %v", text, candidates)
				}
				seen[text] = true
			}
		})
	}
}
