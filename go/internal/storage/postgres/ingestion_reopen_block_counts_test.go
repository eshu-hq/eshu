// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"errors"
	"log"
	"regexp"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
)

// reopenSpanSummary is one relationship.backfill.reopen span read back.
type reopenSpanSummary struct {
	domain string
	pass   string
	ints   map[string]int64
	failed bool
}

// reopenSpans returns every ended relationship.backfill.reopen span, in end
// order, with its domain, pass tag, and int attributes.
func reopenSpans(recorder *tracetest.SpanRecorder) []reopenSpanSummary {
	var out []reopenSpanSummary
	for _, span := range recorder.Ended() {
		if span.Name() != reopenBlockSpanName {
			continue
		}
		out = append(out, summarizeReopenSpan(span))
	}
	return out
}

func summarizeReopenSpan(span sdktrace.ReadOnlySpan) reopenSpanSummary {
	summary := reopenSpanSummary{ints: map[string]int64{}, failed: span.Status().Code == codes.Error}
	for _, attr := range span.Attributes() {
		switch string(attr.Key) {
		case "domain":
			summary.domain = attr.Value.AsString()
		case "pass":
			summary.pass = attr.Value.AsString()
		default:
			summary.ints[string(attr.Key)] = attr.Value.AsInt64()
		}
	}
	return summary
}

// reopenListingRows is the (work_item_id, scope_id, generation_id) shape both
// relationship reopen listings scan.
func reopenListingRows(items ...[3]string) queueFakeRows {
	rows := make([][]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, []any{item[0], item[1], item[2]})
	}
	return queueFakeRows{rows: rows}
}

func singleReopenSpan(t *testing.T, recorder *tracetest.SpanRecorder) reopenSpanSummary {
	t.Helper()
	spans := reopenSpans(recorder)
	if len(spans) != 1 {
		t.Fatalf("reopen spans = %d, want 1", len(spans))
	}
	return spans[0]
}

// TestReopenBlockSpansReportReopenedAndSkippedCounts pins the VALUES of the
// reopen block's reopened and skipped_by_memo attributes against a fixture, not
// only their presence: three succeeded items, one of whose partition the
// same-pass skip-set contains, must report reopened=2 and skipped_by_memo=1,
// and a correlation domain (never memo-gated) must report every item reopened.
func TestReopenBlockSpansReportReopenedAndSkippedCounts(t *testing.T) {
	t.Parallel()

	items := reopenListingRows(
		[3]string{"wi-1", "scope-a", "gen-a"},
		[3]string{"wi-2", "scope-b", "gen-b"},
		[3]string{"wi-3", "scope-c", "gen-c"},
	)
	skipped := map[backfill.ScopeGenerationPartition]struct{}{
		{ScopeID: "scope-a", GenerationID: "gen-a"}: {},
	}

	t.Run("deployment_mapping", func(t *testing.T) {
		t.Parallel()
		recorder, tracer := recordingTracer()
		store := NewIngestionStore(&fakeTx{queryResponses: []queueFakeRows{items}})
		if err := store.reopenDeploymentMappingWorkItemsWithSkipSet(context.Background(), tracer, nil, skipped); err != nil {
			t.Fatalf("reopen error = %v, want nil", err)
		}
		got := singleReopenSpan(t, recorder)
		if got.domain != "deployment_mapping" || got.ints["reopened"] != 2 || got.ints["skipped_by_memo"] != 1 {
			t.Fatalf("reopen span = %+v, want deployment_mapping reopened=2 skipped_by_memo=1", got)
		}
	})
	t.Run("code_import_repo_edge", func(t *testing.T) {
		t.Parallel()
		recorder, tracer := recordingTracer()
		store := NewIngestionStore(&fakeTx{queryResponses: []queueFakeRows{items}})
		if err := store.reopenCodeImportRepoEdgeWorkItemsWithSkipSet(context.Background(), tracer, nil, skipped); err != nil {
			t.Fatalf("reopen error = %v, want nil", err)
		}
		got := singleReopenSpan(t, recorder)
		if got.domain != "code_import_repo_edge" || got.ints["reopened"] != 2 || got.ints["skipped_by_memo"] != 1 {
			t.Fatalf("reopen span = %+v, want code_import_repo_edge reopened=2 skipped_by_memo=1", got)
		}
	})
	t.Run("correlation", func(t *testing.T) {
		t.Parallel()
		recorder, tracer := recordingTracer()
		ids := queueFakeRows{rows: [][]any{{"wi-1"}, {"wi-2"}, {"wi-3"}}}
		store := NewIngestionStore(&fakeTx{queryResponses: []queueFakeRows{ids}})
		if err := store.ReopenSucceededReducerWorkItems(
			context.Background(), tracer, nil, []string{"container_image_identity"},
		); err != nil {
			t.Fatalf("reopen error = %v, want nil", err)
		}
		got := singleReopenSpan(t, recorder)
		if got.domain != "container_image_identity" || got.ints["reopened"] != 3 || got.ints["skipped_by_memo"] != 0 {
			t.Fatalf("reopen span = %+v, want container_image_identity reopened=3 skipped_by_memo=0", got)
		}
	})
}

// TestReopenBlockFailureEndsSpanWithErrorAndLogsFailedLine pins the reopen
// error path: a failing reopen exec ends the block span with Error status,
// emits one deferred_backfill_block_failed line carrying the quoted error, and
// emits no completed line for the block.
func TestReopenBlockFailureEndsSpanWithErrorAndLogsFailedLine(t *testing.T) {
	original := log.Writer()
	t.Cleanup(func() { log.SetOutput(original) })
	var buf bytes.Buffer
	log.SetOutput(&buf)

	recorder, tracer := recordingTracer()
	tx := &fakeTx{
		queryResponses: []queueFakeRows{reopenListingRows([3]string{"wi-1", "scope-a", "gen-a"})},
		execErrors:     map[int]error{0: errors.New("boom reopen exec")},
	}
	store := NewIngestionStore(tx)
	if err := store.reopenDeploymentMappingWorkItemsWithSkipSet(context.Background(), tracer, nil, nil); err == nil {
		t.Fatal("reopen error = nil, want the injected exec failure")
	}

	got := singleReopenSpan(t, recorder)
	if !got.failed {
		t.Fatalf("reopen span = %+v, want Error status", got)
	}
	output := buf.String()
	failed := regexp.MustCompile(
		`deferred_backfill_block_failed block=reopen duration_s=\d+\.\d{2} error="reopen deployment_mapping work items: .*boom reopen exec`,
	).FindAllString(output, -1)
	if len(failed) != 1 {
		t.Fatalf("block_failed reopen lines = %d, want 1\noutput:\n%s", len(failed), output)
	}
	if strings.Contains(output, "deferred_backfill_block_completed block=reopen") {
		t.Errorf("failed reopen emitted a completed line\noutput:\n%s", output)
	}
}
