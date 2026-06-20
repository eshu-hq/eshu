package reducer

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// CodeTaintEvidenceInput is one resolved value-flow taint finding loaded for a
// scope generation: a finding already joined to the Function entity uid it
// concerns by the collector.
type CodeTaintEvidenceInput struct {
	FunctionUID  string
	FunctionName string
	RelativePath string
	Language     string
	Kind         string
	SinkKind     string
	SourceKind   string
	Binding      string
	SourceLine   int
	SinkLine     int
	Confidence   float64
	ClassContext string
	SinkLabel    string
	SourceLabel  string
	GuardReason  string
}

// ExtractCodeTaintEvidenceRows projects taint findings into deterministic graph
// rows. A finding without a resolved Function uid is dropped (it has no node to
// attach to). Rows are keyed by a generation-independent uid so reprojection is
// idempotent, and sorted by uid for byte-stable output.
func ExtractCodeTaintEvidenceRows(inputs []CodeTaintEvidenceInput) []map[string]any {
	rows := make([]map[string]any, 0, len(inputs))
	for _, in := range inputs {
		if in.FunctionUID == "" {
			continue
		}
		rows = append(rows, map[string]any{
			"uid":           codeTaintEvidenceUID(in),
			"function_uid":  in.FunctionUID,
			"function_name": in.FunctionName,
			"relative_path": in.RelativePath,
			"language":      in.Language,
			"kind":          in.Kind,
			"sink_kind":     in.SinkKind,
			"source_kind":   in.SourceKind,
			"binding":       in.Binding,
			"source_line":   in.SourceLine,
			"sink_line":     in.SinkLine,
			"confidence":    in.Confidence,
			"class_context": in.ClassContext,
			"sink_label":    in.SinkLabel,
			"source_label":  in.SourceLabel,
			"guard_reason":  in.GuardReason,
		})
	}
	sort.Slice(rows, func(a, b int) bool {
		return anyToString(rows[a]["uid"]) < anyToString(rows[b]["uid"])
	})
	return rows
}

// codeTaintEvidenceUID derives the generation-independent node identity of one
// finding: a source-to-sink flow within a function, identified by the function
// uid, the source/sink lines, the sink and source kinds, and the binding.
func codeTaintEvidenceUID(in CodeTaintEvidenceInput) string {
	return facts.StableID("CodeTaintEvidence", map[string]any{
		"function_uid": in.FunctionUID,
		"source_line":  in.SourceLine,
		"sink_line":    in.SinkLine,
		"sink_kind":    in.SinkKind,
		"source_kind":  in.SourceKind,
		"binding":      in.Binding,
	})
}
