package dataflowemit

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
)

// CatalogVersionRow renders one value-flow catalog content version into a
// "dataflow_catalog_versions" payload row.
func CatalogVersionRow(lang, catalog, version string) map[string]any {
	return map[string]any{
		"lang":    lang,
		"catalog": catalog,
		"version": version,
	}
}

// DataflowFunctionRow renders one function's control-flow graph and resolved
// def->use facts into a "dataflow_functions" payload row. lang labels the row's
// source language; classContext is the enclosing type/class, omitted when empty.
func DataflowFunctionRow(lang, name string, line int, classContext string, fn cfg.Function) map[string]any {
	row := map[string]any{
		"name":        name,
		"line_number": line,
		"lang":        lang,
		"blocks":      blockPayloads(fn.Blocks),
		"def_uses":    defUsePayloads(fn.DefUses),
	}
	if classContext != "" {
		row["class_context"] = classContext
	}
	if fn.Overflow.Any() {
		row["overflow"] = map[string]any{
			"blocks":               fn.Overflow.Blocks,
			"stmts":                fn.Overflow.Stmts,
			"def_use_edges":        fn.Overflow.DefUseEdges,
			"control_dependencies": fn.Overflow.ControlDependencies,
			"access_paths":         fn.Overflow.AccessPaths,
		}
	}
	return row
}

// TaintFindingRow renders one intraprocedural taint finding into a
// "taint_findings" payload row with confidence and provenance.
func TaintFindingRow(lang, name string, line int, classContext string, finding taint.Finding) map[string]any {
	row := map[string]any{
		"function_name": name,
		"line_number":   line,
		"lang":          lang,
		"kind":          string(finding.Kind),
		"sink_kind":     string(finding.SinkKind),
		"source_kind":   finding.SourceKind,
		"binding":       finding.Binding,
		"source_line":   finding.SourceLine,
		"sink_line":     finding.SinkLine,
		"confidence":    finding.Confidence,
	}
	if classContext != "" {
		row["class_context"] = classContext
	}
	if finding.SinkLabel != "" {
		row["sink_label"] = finding.SinkLabel
	}
	if finding.SourceLabel != "" {
		row["source_label"] = finding.SourceLabel
	}
	if finding.GuardReason != "" {
		row["guard_reason"] = finding.GuardReason
	}
	if len(finding.Neutralized) > 0 {
		neutralized := make([]string, 0, len(finding.Neutralized))
		for _, kind := range finding.Neutralized {
			neutralized = append(neutralized, string(kind))
		}
		row["neutralized"] = neutralized
	}
	return row
}

// InterprocFindingRow renders one interprocedural finding into an
// "interproc_findings" payload row. The "cloud" flag is omitted when false.
func InterprocFindingRow(lang string, finding interproc.Finding) map[string]any {
	row := map[string]any{
		"source_func": string(finding.SourceFunc),
		"source_kind": finding.SourceKind,
		"sink_func":   string(finding.SinkFunc),
		"sink_kind":   string(finding.SinkKind),
		"confidence":  finding.Confidence,
		"lang":        lang,
	}
	if finding.Cloud {
		row["cloud"] = true
	}
	if len(finding.Trail) > 0 {
		row["why_trail"] = interprocTrailPayload(finding.Trail)
	}
	if finding.TrailTruncated {
		row["why_trail_truncated"] = true
	}
	return row
}

// DataflowSummaryRow renders one function's structural value-flow Effects into a
// "dataflow_summaries" payload row. lang labels the row's source language. The row
// carries the durable FunctionID and the raw effects (param→return, param→sink,
// source→return, param→callee-arg); the content version is computed later by the
// reducer's summary.Store, not here. Empty effect lists are omitted so a
// finding-free function still round-trips byte-stably.
func DataflowSummaryRow(lang string, id summary.FunctionID, effects summary.Effects) map[string]any {
	row := map[string]any{
		"function_id": string(id),
		"lang":        lang,
	}
	if len(effects.ParamToReturn) > 0 {
		row["param_to_return"] = effects.ParamToReturn
	}
	if len(effects.ParamToSink) > 0 {
		sinks := make([]map[string]any, 0, len(effects.ParamToSink))
		for _, s := range effects.ParamToSink {
			sinks = append(sinks, map[string]any{"param": s.Param, "sink_kind": s.SinkKind})
		}
		row["param_to_sink"] = sinks
	}
	if len(effects.SourceToReturn) > 0 {
		row["source_to_return"] = effects.SourceToReturn
	}
	if len(effects.ParamToCallArg) > 0 {
		calls := make([]map[string]any, 0, len(effects.ParamToCallArg))
		for _, c := range effects.ParamToCallArg {
			calls = append(calls, map[string]any{"callee": string(c.Callee), "param": c.Param, "arg": c.Arg})
		}
		row["param_to_call_arg"] = calls
	}
	return row
}

// DataflowSourceRow renders one interprocedural taint source — a parameter port
// that is a taint entry point (e.g. an *http.Request param) — into a
// "dataflow_sources" payload row. lang labels the row's source language. The row
// carries the durable FunctionID, the parameter index, and the source kind, so
// the cross-repo fixpoint can reconstruct the entry points the per-file analysis
// derived from the AST but does not otherwise persist.
func DataflowSourceRow(lang string, src interproc.Source) map[string]any {
	return map[string]any{
		"function_id": string(src.Port.Func),
		"param_index": src.Port.Slot.Index,
		"kind":        src.Kind,
		"lang":        lang,
	}
}

func interprocTrailPayload(trail []interproc.Port) []map[string]any {
	out := make([]map[string]any, 0, len(trail))
	for _, port := range trail {
		step := map[string]any{
			"function_id": string(port.Func),
			"slot_kind":   interprocSlotKindString(port.Slot.Kind),
		}
		if port.Slot.Kind == interproc.SlotParam {
			step["slot_index"] = port.Slot.Index
		}
		if port.Slot.Name != "" {
			step["slot_name"] = port.Slot.Name
		}
		out = append(out, step)
	}
	return out
}

func interprocSlotKindString(kind interproc.SlotKind) string {
	switch kind {
	case interproc.SlotParam:
		return "param"
	case interproc.SlotReturn:
		return "return"
	case interproc.SlotNamed:
		return "named"
	default:
		return "unknown"
	}
}

// SortSourceRows orders "dataflow_sources" rows by (function_id, param_index) so
// the bucket is byte-stable across runs.
func SortSourceRows(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool {
		if fi, fj := stringFromRow(rows[i], "function_id"), stringFromRow(rows[j], "function_id"); fi != fj {
			return fi < fj
		}
		return intFromRow(rows[i], "param_index") < intFromRow(rows[j], "param_index")
	})
}

// blockPayloads renders basic blocks with their statements and sorted successors.
func blockPayloads(blocks []cfg.Block) []map[string]any {
	out := make([]map[string]any, 0, len(blocks))
	for _, block := range blocks {
		stmts := make([]map[string]any, 0, len(block.Stmts))
		for _, stmt := range block.Stmts {
			stmts = append(stmts, map[string]any{
				"id":   stmt.ID,
				"line": stmt.Line,
				"defs": stmt.Defs,
				"uses": stmt.Uses,
			})
		}
		out = append(out, map[string]any{
			"id":    block.ID,
			"succs": block.Succs,
			"stmts": stmts,
		})
	}
	return out
}

// defUsePayloads renders resolved def->use edges in their already-sorted order.
func defUsePayloads(defUses []cfg.DefUse) []map[string]any {
	out := make([]map[string]any, 0, len(defUses))
	for _, du := range defUses {
		out = append(out, map[string]any{
			"binding":  du.Binding,
			"def_stmt": du.DefStmt,
			"def_line": du.DefLine,
			"use_stmt": du.UseStmt,
			"use_line": du.UseLine,
		})
	}
	return out
}

// SortFunctionRows orders "dataflow_functions" rows by (line_number, name) so the
// bucket is byte-stable across runs.
func SortFunctionRows(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool {
		li, lj := intFromRow(rows[i], "line_number"), intFromRow(rows[j], "line_number")
		if li != lj {
			return li < lj
		}
		return stringFromRow(rows[i], "name") < stringFromRow(rows[j], "name")
	})
}

// SortFindingRows orders "taint_findings" rows deterministically by sink line,
// source line, binding, and kind.
func SortFindingRows(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool {
		if si, sj := intFromRow(rows[i], "sink_line"), intFromRow(rows[j], "sink_line"); si != sj {
			return si < sj
		}
		if si, sj := intFromRow(rows[i], "source_line"), intFromRow(rows[j], "source_line"); si != sj {
			return si < sj
		}
		if bi, bj := stringFromRow(rows[i], "binding"), stringFromRow(rows[j], "binding"); bi != bj {
			return bi < bj
		}
		return stringFromRow(rows[i], "kind") < stringFromRow(rows[j], "kind")
	})
}

// SortSummaryRows orders "dataflow_summaries" rows by function_id so the bucket is
// byte-stable across runs.
func SortSummaryRows(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool {
		return stringFromRow(rows[i], "function_id") < stringFromRow(rows[j], "function_id")
	})
}

// intFromRow reads an int payload field.
func intFromRow(row map[string]any, key string) int {
	value, _ := row[key].(int)
	return value
}

// stringFromRow reads a string payload field.
func stringFromRow(row map[string]any, key string) string {
	value, _ := row[key].(string)
	return value
}
