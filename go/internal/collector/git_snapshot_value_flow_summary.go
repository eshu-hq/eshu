package collector

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/summary"
)

// buildValueFlowSummaries extracts parser-emitted dataflow_summaries rows from
// parsed files. Rows without stable repository, package, and function identity
// are ignored at the collector boundary so malformed parser payloads do not
// create ambiguous durable summary rows.
func buildValueFlowSummaries(parsedFiles []map[string]any) []ValueFlowSummarySnapshot {
	var out []ValueFlowSummarySnapshot
	for _, parsedFile := range parsedFiles {
		for _, row := range summaryRows(parsedFile["dataflow_summaries"]) {
			id := summary.FunctionID(snapshotPayloadString(row, "function_id"))
			if !validValueFlowSummaryID(id) {
				continue
			}
			out = append(out, ValueFlowSummarySnapshot{
				FunctionID: id,
				Effects: summary.Effects{
					ParamToReturn:  intsFromPayload(row["param_to_return"]),
					ParamToSink:    paramSinksFromPayload(row["param_to_sink"]),
					SourceToReturn: stringsFromPayload(row["source_to_return"]),
					ParamToCallArg: callArgFlowsFromPayload(row["param_to_call_arg"]),
				},
				Language: snapshotPayloadString(row, "lang", "language"),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FunctionID < out[j].FunctionID })
	return out
}

// buildValueFlowSources extracts parser-emitted dataflow_sources rows from
// parsed files. Rows without stable function identity, param index, or source
// kind are ignored at the collector boundary.
func buildValueFlowSources(parsedFiles []map[string]any) []ValueFlowSourceSnapshot {
	var out []ValueFlowSourceSnapshot
	for _, parsedFile := range parsedFiles {
		for _, row := range summaryRows(parsedFile["dataflow_sources"]) {
			id := summary.FunctionID(snapshotPayloadString(row, "function_id"))
			param, ok := intFromPayload(row["param_index"])
			kind := snapshotPayloadString(row, "source_kind", "kind")
			if !validValueFlowSummaryID(id) || !ok || param < 0 || strings.TrimSpace(kind) == "" {
				continue
			}
			out = append(out, ValueFlowSourceSnapshot{
				FunctionID: id,
				ParamIndex: param,
				Kind:       kind,
				Label:      snapshotPayloadString(row, "source_label", "label"),
				Language:   snapshotPayloadString(row, "lang", "language"),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FunctionID != out[j].FunctionID {
			return out[i].FunctionID < out[j].FunctionID
		}
		if out[i].ParamIndex != out[j].ParamIndex {
			return out[i].ParamIndex < out[j].ParamIndex
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Label < out[j].Label
	})
	return out
}

func summaryRows(value any) []map[string]any {
	switch rows := value.(type) {
	case []map[string]any:
		return rows
	case []any:
		out := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			mapped, ok := row.(map[string]any)
			if ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

func validValueFlowSummaryID(id summary.FunctionID) bool {
	parts := strings.Split(string(id), "\x1f")
	return len(parts) == 4 && strings.TrimSpace(parts[0]) != "" &&
		strings.TrimSpace(parts[1]) != "" && strings.TrimSpace(parts[3]) != ""
}

func intsFromPayload(value any) []int {
	switch values := value.(type) {
	case []int:
		return append([]int(nil), values...)
	case []any:
		out := make([]int, 0, len(values))
		for _, value := range values {
			if converted, ok := intFromPayload(value); ok {
				out = append(out, converted)
			}
		}
		return out
	default:
		return nil
	}
}

func stringsFromPayload(value any) []string {
	switch values := value.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if ok && strings.TrimSpace(text) != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func paramSinksFromPayload(value any) []summary.ParamSink {
	var rows []map[string]any
	switch values := value.(type) {
	case []map[string]any:
		rows = values
	case []any:
		for _, value := range values {
			row, ok := value.(map[string]any)
			if ok {
				rows = append(rows, row)
			}
		}
	}
	out := make([]summary.ParamSink, 0, len(rows))
	for _, row := range rows {
		param, ok := intFromPayload(row["param"])
		sinkKind := snapshotPayloadString(row, "sink_kind")
		if !ok || sinkKind == "" {
			continue
		}
		out = append(out, summary.ParamSink{Param: param, SinkKind: sinkKind})
	}
	return out
}

func callArgFlowsFromPayload(value any) []summary.CallArgFlow {
	var rows []map[string]any
	switch values := value.(type) {
	case []map[string]any:
		rows = values
	case []any:
		for _, value := range values {
			row, ok := value.(map[string]any)
			if ok {
				rows = append(rows, row)
			}
		}
	}
	out := make([]summary.CallArgFlow, 0, len(rows))
	for _, row := range rows {
		param, okParam := intFromPayload(row["param"])
		arg, okArg := intFromPayload(row["arg"])
		callee := summary.FunctionID(snapshotPayloadString(row, "callee"))
		if !okParam || !okArg || !validValueFlowSummaryID(callee) {
			continue
		}
		out = append(out, summary.CallArgFlow{Callee: callee, Param: param, Arg: arg})
	}
	return out
}

func intFromPayload(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		if typed != float64(int(typed)) {
			return 0, false
		}
		return int(typed), true
	default:
		return 0, false
	}
}
