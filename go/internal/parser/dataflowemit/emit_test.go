package dataflowemit

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
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
	for _, key := range []string{"class_context", "sink_label", "source_label", "neutralized"} {
		if _, present := bare[key]; present {
			t.Fatalf("optional field %q present when empty: %+v", key, bare)
		}
	}

	full := TaintFindingRow("python", "method", 9, "Repo", taint.Finding{
		Kind: taint.FindingTainted, SinkKind: "sql", SinkLabel: "execute",
		SourceLabel: "request", Neutralized: []taint.Kind{"html"},
	})
	if full["class_context"] != "Repo" || full["sink_label"] != "execute" || full["source_label"] != "request" {
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
