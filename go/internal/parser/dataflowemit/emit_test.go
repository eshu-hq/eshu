package dataflowemit

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
)

// TestTaintFindingRowOmitsEmptyOptionalFields proves optional provenance fields
// are absent when empty and present when set, and that the lang label is carried.
func TestTaintFindingRowOmitsEmptyOptionalFields(t *testing.T) {
	bare := TaintFindingRow("python", "view", 3, "", taint.Finding{
		Kind: taint.FindingTainted, SinkKind: "sql", Binding: "q",
	})
	if bare["lang"] != "python" {
		t.Fatalf("lang = %v, want python", bare["lang"])
	}
	for _, key := range []string{"class_context", "sink_label", "source_label", "guard_reason", "neutralized"} {
		if _, present := bare[key]; present {
			t.Fatalf("optional field %q present when empty: %+v", key, bare)
		}
	}

	full := TaintFindingRow("python", "method", 9, "Repo", taint.Finding{
		Kind: taint.FindingTainted, SinkKind: "sql", SinkLabel: "execute",
		SourceLabel: "request", GuardReason: "allowed", Neutralized: []taint.Kind{"html"},
	})
	if full["class_context"] != "Repo" || full["sink_label"] != "execute" ||
		full["source_label"] != "request" || full["guard_reason"] != "allowed" {
		t.Fatalf("optional fields not carried when set: %+v", full)
	}
	if got, _ := full["neutralized"].([]string); len(got) != 1 || got[0] != "html" {
		t.Fatalf("neutralized = %v, want [html]", full["neutralized"])
	}
}

// TestInterprocFindingRowCloudOmitted proves the cloud flag is omitted unless the
// finding is a code-to-cloud terminal.
func TestInterprocFindingRowCloudOmitted(t *testing.T) {
	row := InterprocFindingRow("python", interproc.Finding{
		SourceFunc: "view", SinkFunc: "run_query", SinkKind: "sql", Confidence: 0.6,
	})
	if _, present := row["cloud"]; present {
		t.Fatalf("cloud present when false: %+v", row)
	}
	if row["lang"] != "python" || row["sink_kind"] != "sql" {
		t.Fatalf("unexpected row: %+v", row)
	}

	cloud := InterprocFindingRow("go", interproc.Finding{SourceFunc: "h", SinkFunc: "s", Cloud: true})
	if cloud["cloud"] != true {
		t.Fatalf("cloud flag not set: %+v", cloud)
	}
}

// TestInterprocFindingRowCarriesWhyTrail proves the parser payload preserves the
// ordered finding trail without adding optional fields when it is absent.
func TestInterprocFindingRowCarriesWhyTrail(t *testing.T) {
	src := interproc.Port{Func: "repo\x1fview", Slot: interproc.Slot{Kind: interproc.SlotParam, Index: 0}}
	mid := interproc.Port{Func: "repo\x1fservice", Slot: interproc.Slot{Kind: interproc.SlotNamed, Name: "payload"}}
	sink := interproc.Port{Func: "repo\x1frun_query", Slot: interproc.Slot{Kind: interproc.SlotReturn}}
	row := InterprocFindingRow("go", interproc.Finding{
		SourceFunc: "repo\x1fview",
		SinkFunc:   "repo\x1frun_query",
		SinkKind:   "sql",
		Trail:      []interproc.Port{src, mid, sink},
	})

	trail, ok := row["why_trail"].([]map[string]any)
	if !ok {
		t.Fatalf("why_trail type = %T, want []map[string]any", row["why_trail"])
	}
	if len(trail) != 3 {
		t.Fatalf("len(why_trail) = %d, want 3: %+v", len(trail), trail)
	}
	if trail[0]["function_id"] != "repo\x1fview" || trail[0]["slot_kind"] != "param" || trail[0]["slot_index"] != 0 {
		t.Fatalf("source trail step not rendered: %+v", trail[0])
	}
	if trail[1]["slot_name"] != "payload" || trail[2]["slot_kind"] != "return" {
		t.Fatalf("intermediate/sink trail steps not rendered: %+v", trail)
	}

	truncated := InterprocFindingRow("go", interproc.Finding{Trail: []interproc.Port{src}, TrailTruncated: true})
	if truncated["why_trail_truncated"] != true {
		t.Fatalf("why_trail_truncated not carried: %+v", truncated)
	}
}

// TestSortFindingRowsDeterministic proves findings sort by sink line, then source
// line, then binding, then kind.
func TestSortFindingRowsDeterministic(t *testing.T) {
	rows := []map[string]any{
		{"sink_line": 10, "source_line": 1, "binding": "b", "kind": "TAINTED"},
		{"sink_line": 5, "source_line": 2, "binding": "a", "kind": "TAINTED"},
		{"sink_line": 5, "source_line": 2, "binding": "a", "kind": "SANITIZED"},
	}
	SortFindingRows(rows)
	if rows[0]["sink_line"] != 5 || rows[0]["kind"] != "SANITIZED" {
		t.Fatalf("unexpected order: %+v", rows)
	}
	if rows[2]["sink_line"] != 10 {
		t.Fatalf("highest sink line should sort last: %+v", rows)
	}
}

func TestDataflowFunctionRowCarriesControlDependencyOverflow(t *testing.T) {
	row := DataflowFunctionRow("go", "handler", 3, "", cfg.Function{
		Overflow: cfg.Overflow{ControlDependencies: 2},
	})
	overflow, ok := row["overflow"].(map[string]any)
	if !ok {
		t.Fatalf("overflow missing: %+v", row)
	}
	if got := overflow["control_dependencies"]; got != 2 {
		t.Fatalf("control_dependencies overflow = %v, want 2", got)
	}
}

// TestDataflowSummaryRowRendersEffectsAndOmitsEmpty proves the summary row
// carries the FunctionID and each non-empty effect list (nested for param_to_sink
// and param_to_call_arg), and omits empty effect lists.
func TestDataflowSummaryRowRendersEffectsAndOmitsEmpty(t *testing.T) {
	bare := DataflowSummaryRow("go", summary.FunctionID("repo\x1fpkg\x1f\x1ffree"), summary.Effects{})
	if bare["function_id"] != "repo\x1fpkg\x1f\x1ffree" || bare["lang"] != "go" {
		t.Fatalf("identity/lang not carried: %+v", bare)
	}
	for _, key := range []string{"param_to_return", "param_to_sink", "source_to_return", "param_to_call_arg"} {
		if _, present := bare[key]; present {
			t.Fatalf("empty effect %q must be omitted: %+v", key, bare)
		}
	}

	full := DataflowSummaryRow("go", summary.FunctionID("repo\x1fpkg\x1fA\x1fhandle"), summary.Effects{
		ParamToReturn:  []int{0},
		ParamToSink:    []summary.ParamSink{{Param: 1, SinkKind: "sql"}},
		SourceToReturn: []string{"http_request"},
		ParamToCallArg: []summary.CallArgFlow{{Callee: summary.FunctionID("repo\x1fpkg\x1f\x1fquery"), Param: 0, Arg: 1}},
	})
	sinks, _ := full["param_to_sink"].([]map[string]any)
	if len(sinks) != 1 || sinks[0]["sink_kind"] != "sql" || sinks[0]["param"] != 1 {
		t.Fatalf("param_to_sink not rendered: %+v", full["param_to_sink"])
	}
	calls, _ := full["param_to_call_arg"].([]map[string]any)
	if len(calls) != 1 || calls[0]["callee"] != "repo\x1fpkg\x1f\x1fquery" || calls[0]["arg"] != 1 {
		t.Fatalf("param_to_call_arg not rendered: %+v", full["param_to_call_arg"])
	}
}

// TestSortSummaryRowsByFunctionID proves rows are ordered by function_id.
func TestSortSummaryRowsByFunctionID(t *testing.T) {
	rows := []map[string]any{
		{"function_id": "z"}, {"function_id": "a"}, {"function_id": "m"},
	}
	SortSummaryRows(rows)
	if rows[0]["function_id"] != "a" || rows[1]["function_id"] != "m" || rows[2]["function_id"] != "z" {
		t.Fatalf("not sorted by function_id: %+v", rows)
	}
}

// TestDataflowSourceRowRendersPortAndKind proves the source row carries the
// FunctionID, parameter index, and kind, and that SortSourceRows orders rows.
func TestDataflowSourceRowRendersPortAndKind(t *testing.T) {
	row := DataflowSourceRow("go", interproc.Source{
		Port: interproc.Port{Func: interproc.FunctionID("repo\x1fpkg\x1f\x1fhandle"), Slot: interproc.Slot{Kind: interproc.SlotParam, Index: 2}},
		Kind: "http_request",
	})
	if row["function_id"] != "repo\x1fpkg\x1f\x1fhandle" || row["param_index"] != 2 || row["kind"] != "http_request" || row["lang"] != "go" {
		t.Fatalf("source row not rendered: %+v", row)
	}
	rows := []map[string]any{
		{"function_id": "b", "param_index": 0},
		{"function_id": "a", "param_index": 1},
		{"function_id": "a", "param_index": 0},
	}
	SortSourceRows(rows)
	if rows[0]["function_id"] != "a" || rows[0]["param_index"] != 0 || rows[2]["function_id"] != "b" {
		t.Fatalf("SortSourceRows wrong: %+v", rows)
	}
}
