package reducer

import (
	"encoding/json"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// CodeInterprocEvidenceInput is one resolved cross-function value-flow finding
// loaded for a scope generation: a finding whose source and sink functions the
// collector already joined to their Function entity uids.
type CodeInterprocEvidenceInput struct {
	SourceFunctionUID  string
	SinkFunctionUID    string
	RelativePath       string
	SourceFunctionName string
	SinkFunctionName   string
	Language           string
	SinkKind           string
	SourceKind         string
	Confidence         float64
	Cloud              bool
	WhyTrail           []map[string]any
	WhyTrailTruncated  bool
}

// ExtractCodeInterprocEvidenceRows projects cross-function findings into
// deterministic graph edge rows. A finding missing either endpoint uid is dropped
// (no edge to draw). Rows are keyed by a generation-independent edge uid so
// reprojection is idempotent, and sorted by uid for byte-stable output.
func ExtractCodeInterprocEvidenceRows(inputs []CodeInterprocEvidenceInput) []map[string]any {
	return extractCodeInterprocEvidenceRows(inputs, codeInterprocEvidenceUID)
}

// ExtractCodeInterprocFixpointEvidenceRows projects summary-fixpoint findings
// into deterministic edge rows under a separate uid namespace so they cannot
// clobber direct code_interproc_evidence rows in the graph writer's MERGE.
func ExtractCodeInterprocFixpointEvidenceRows(inputs []CodeInterprocEvidenceInput) []map[string]any {
	return extractCodeInterprocEvidenceRows(inputs, codeInterprocFixpointEvidenceUID)
}

func extractCodeInterprocEvidenceRows(inputs []CodeInterprocEvidenceInput, uidForInput func(CodeInterprocEvidenceInput) string) []map[string]any {
	rows := make([]map[string]any, 0, len(inputs))
	for _, in := range inputs {
		if in.SourceFunctionUID == "" || in.SinkFunctionUID == "" {
			continue
		}
		row := map[string]any{
			"uid":                  uidForInput(in),
			"source_function_uid":  in.SourceFunctionUID,
			"sink_function_uid":    in.SinkFunctionUID,
			"relative_path":        in.RelativePath,
			"source_function_name": in.SourceFunctionName,
			"sink_function_name":   in.SinkFunctionName,
			"language":             in.Language,
			"sink_kind":            in.SinkKind,
			"source_kind":          in.SourceKind,
			"confidence":           in.Confidence,
			"cloud":                in.Cloud,
		}
		if len(in.WhyTrail) > 0 {
			row["why_trail_json"] = codeInterprocWhyTrailJSON(in.WhyTrail)
		}
		if in.WhyTrailTruncated {
			row["why_trail_truncated"] = true
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(a, b int) bool {
		return anyToString(rows[a]["uid"]) < anyToString(rows[b]["uid"])
	})
	return rows
}

func codeInterprocWhyTrailJSON(trail []map[string]any) string {
	if len(trail) == 0 {
		return ""
	}
	encoded, err := json.Marshal(trail)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// codeInterprocEvidenceUID derives the generation-independent identity of one
// cross-function flow: the source and sink function uids plus the sink and source
// kinds. Distinct flows between the same pair (different kinds) get distinct uids.
func codeInterprocEvidenceUID(in CodeInterprocEvidenceInput) string {
	return facts.StableID("CodeInterprocEvidence", map[string]any{
		"source_function_uid": in.SourceFunctionUID,
		"sink_function_uid":   in.SinkFunctionUID,
		"sink_kind":           in.SinkKind,
		"source_kind":         in.SourceKind,
	})
}

func codeInterprocFixpointEvidenceUID(in CodeInterprocEvidenceInput) string {
	return facts.StableID("CodeInterprocFixpointEvidence", map[string]any{
		"source_function_uid": in.SourceFunctionUID,
		"sink_function_uid":   in.SinkFunctionUID,
		"sink_kind":           in.SinkKind,
		"source_kind":         in.SourceKind,
	})
}
