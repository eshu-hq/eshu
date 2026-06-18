package collector

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestBuildValueFlowSummariesMapsParserRows(t *testing.T) {
	t.Parallel()

	functionID := summary.NewFunctionID("repo-alpha", "example.com/repo-alpha/pkg", "", "Handle")
	calleeID := summary.NewFunctionID("repo-alpha", "example.com/repo-alpha/pkg", "", "Query")
	parsedFiles := []map[string]any{{
		"path": "/repo/handler.go",
		"dataflow_summaries": []map[string]any{{
			"function_id":      string(functionID),
			"lang":             "go",
			"param_to_return":  []any{float64(0)},
			"param_to_sink":    []map[string]any{{"param": float64(1), "sink_kind": "sql"}},
			"source_to_return": []any{"env"},
			"param_to_call_arg": []map[string]any{{
				"callee": string(calleeID),
				"param":  float64(0),
				"arg":    float64(1),
			}},
		}},
	}}

	got := buildValueFlowSummaries(parsedFiles)
	if len(got) != 1 {
		t.Fatalf("summary count = %d, want 1", len(got))
	}
	if got[0].FunctionID != functionID {
		t.Fatalf("FunctionID = %q, want %q", got[0].FunctionID, functionID)
	}
	want := summary.Effects{
		ParamToReturn:  []int{0},
		ParamToSink:    []summary.ParamSink{{Param: 1, SinkKind: "sql"}},
		SourceToReturn: []string{"env"},
		ParamToCallArg: []summary.CallArgFlow{{Callee: calleeID, Param: 0, Arg: 1}},
	}
	if !equalSummaryEffects(got[0].Effects, want) {
		t.Fatalf("Effects = %#v, want %#v", got[0].Effects, want)
	}
}

func TestBuildValueFlowSummariesDropsMalformedRows(t *testing.T) {
	t.Parallel()

	parsedFiles := []map[string]any{{
		"dataflow_summaries": []map[string]any{
			{"function_id": "", "lang": "go", "param_to_return": []any{float64(0)}},
			{"function_id": "handler", "lang": "go", "param_to_return": []any{float64(0)}},
		},
	}}

	if got := buildValueFlowSummaries(parsedFiles); len(got) != 0 {
		t.Fatalf("summary count = %d, want 0 for malformed rows", len(got))
	}
}

func TestBuildValueFlowSourcesMapsParserRows(t *testing.T) {
	t.Parallel()

	functionID := summary.NewFunctionID("repo-alpha", "example.com/repo-alpha/pkg", "", "Handle")
	parsedFiles := []map[string]any{{
		"dataflow_sources": []map[string]any{{
			"function_id":  string(functionID),
			"lang":         "go",
			"param_index":  float64(0),
			"source_kind":  "http_request",
			"source_label": "request",
		}},
	}}

	got := buildValueFlowSources(parsedFiles)
	if len(got) != 1 {
		t.Fatalf("source count = %d, want 1", len(got))
	}
	if got[0].FunctionID != functionID {
		t.Fatalf("FunctionID = %q, want %q", got[0].FunctionID, functionID)
	}
	if got[0].ParamIndex != 0 || got[0].Kind != "http_request" || got[0].Label != "request" {
		t.Fatalf("source row = %#v, want param 0 http_request request", got[0])
	}
}

func TestBuildValueFlowSourcesDropsMalformedRows(t *testing.T) {
	t.Parallel()

	parsedFiles := []map[string]any{{
		"dataflow_sources": []map[string]any{
			{"function_id": "", "lang": "go", "param_index": float64(0), "source_kind": "http_request"},
			{"function_id": "handler", "lang": "go", "param_index": float64(0), "source_kind": "http_request"},
			{"function_id": string(summary.NewFunctionID("repo-alpha", "example.com/pkg", "", "Handle")), "lang": "go", "source_kind": "http_request"},
			{"function_id": string(summary.NewFunctionID("repo-alpha", "example.com/pkg", "", "Handle")), "lang": "go", "param_index": float64(0)},
		},
	}}

	if got := buildValueFlowSources(parsedFiles); len(got) != 0 {
		t.Fatalf("source count = %d, want 0 for malformed rows", len(got))
	}
}

func TestSnapshotFreshnessHintIncludesValueFlowSummaries(t *testing.T) {
	t.Parallel()

	functionID := summary.NewFunctionID("repo-alpha", "example.com/repo-alpha/pkg", "", "Handle")
	base := RepositorySnapshot{
		FileCount: 1,
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: "handler.go",
			Digest:       "digest-1",
		}},
		ValueFlowSummaries: []ValueFlowSummarySnapshot{{
			FunctionID: functionID,
			Effects:    summary.Effects{ParamToReturn: []int{0}},
			Language:   "go",
		}},
	}
	changed := base
	changed.ValueFlowSummaries = []ValueFlowSummarySnapshot{{
		FunctionID: functionID,
		Effects:    summary.Effects{ParamToSink: []summary.ParamSink{{Param: 0, SinkKind: "sql"}}},
		Language:   "go",
	}}

	if snapshotFreshnessHint(base) == snapshotFreshnessHint(changed) {
		t.Fatal("freshness hints match, want summary structural changes to avoid commit-time skip")
	}
}

func TestSnapshotFreshnessHintIncludesValueFlowSources(t *testing.T) {
	t.Parallel()

	functionID := summary.NewFunctionID("repo-alpha", "example.com/repo-alpha/pkg", "", "Handle")
	base := RepositorySnapshot{
		FileCount: 1,
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: "handler.go",
			Digest:       "digest-1",
		}},
		ValueFlowSources: []ValueFlowSourceSnapshot{{
			FunctionID: functionID,
			ParamIndex: 0,
			Kind:       "http_request",
			Language:   "go",
		}},
	}
	changed := base
	changed.ValueFlowSources = []ValueFlowSourceSnapshot{{
		FunctionID: functionID,
		ParamIndex: 1,
		Kind:       "http_request",
		Language:   "go",
	}}

	if snapshotFreshnessHint(base) == snapshotFreshnessHint(changed) {
		t.Fatal("freshness hints match, want source entry-point changes to avoid commit-time skip")
	}
}

func TestBuildStreamingGenerationDoesNotCountValueFlowSummariesAsFacts(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 18, 6, 0, 0, 0, time.UTC)
	repo := repositoryidentity.Metadata{ID: "repo-alpha", Name: "repo-alpha"}
	withoutSummaries := buildStreamingGenerationWithContext(
		context.Background(),
		t.TempDir(),
		repo,
		"run-1",
		observedAt,
		RepositorySnapshot{FileCount: 1},
		false,
	)
	withSummaries := buildStreamingGenerationWithContext(
		context.Background(),
		t.TempDir(),
		repo,
		"run-1",
		observedAt,
		RepositorySnapshot{
			FileCount: 1,
			ValueFlowSummaries: []ValueFlowSummarySnapshot{{
				FunctionID: summary.NewFunctionID("repo-alpha", "example.com/repo-alpha/pkg", "", "Handle"),
				Effects:    summary.Effects{ParamToReturn: []int{0}},
				Language:   "go",
			}},
		},
		false,
	)

	if withSummaries.FactCount != withoutSummaries.FactCount {
		t.Fatalf("FactCount with summaries = %d, want %d", withSummaries.FactCount, withoutSummaries.FactCount)
	}
	if len(withSummaries.ValueFlowSummaries) != 1 {
		t.Fatalf("ValueFlowSummaries count = %d, want 1", len(withSummaries.ValueFlowSummaries))
	}
	drainFactStream(withSummaries.Facts)
	drainFactStream(withoutSummaries.Facts)
}

func TestBuildStreamingGenerationDoesNotCountValueFlowSourcesAsFacts(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 6, 18, 6, 0, 0, 0, time.UTC)
	repo := repositoryidentity.Metadata{ID: "repo-alpha", Name: "repo-alpha"}
	withoutSources := buildStreamingGenerationWithContext(
		context.Background(),
		t.TempDir(),
		repo,
		"run-1",
		observedAt,
		RepositorySnapshot{FileCount: 1},
		false,
	)
	withSources := buildStreamingGenerationWithContext(
		context.Background(),
		t.TempDir(),
		repo,
		"run-1",
		observedAt,
		RepositorySnapshot{
			FileCount: 1,
			ValueFlowSources: []ValueFlowSourceSnapshot{{
				FunctionID: summary.NewFunctionID("repo-alpha", "example.com/repo-alpha/pkg", "", "Handle"),
				ParamIndex: 0,
				Kind:       "http_request",
				Language:   "go",
			}},
		},
		false,
	)

	if withSources.FactCount != withoutSources.FactCount {
		t.Fatalf("FactCount with sources = %d, want %d", withSources.FactCount, withoutSources.FactCount)
	}
	if len(withSources.ValueFlowSources) != 1 {
		t.Fatalf("ValueFlowSources count = %d, want 1", len(withSources.ValueFlowSources))
	}
	drainFactStream(withSources.Facts)
	drainFactStream(withoutSources.Facts)
}

func equalSummaryEffects(a, b summary.Effects) bool {
	return summary.StructuralHash(a) == summary.StructuralHash(b)
}
