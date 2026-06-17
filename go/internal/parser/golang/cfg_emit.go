package golang

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// goEmitDataflowBuckets lowers every top-level Go function and method once and
// renders two deterministic, bounded payload buckets: per-function control-flow
// and reaching-definition facts ("dataflow_functions"), and intraprocedural
// taint findings ("taint_findings"). Function literals are skipped here;
// closures are modeled by a later pass. Both slices are sorted so the buckets
// are byte-stable across runs.
func goEmitDataflowBuckets(root *tree_sitter.Node, source []byte) (dataflow, findings []map[string]any) {
	limits := cfg.DefaultLimits()
	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "method_declaration":
		default:
			return
		}
		nameNode := node.ChildByFieldName("name")
		name := strings.TrimSpace(nodeText(nameNode, source))
		if name == "" {
			return
		}
		line := nodeLine(nameNode)
		classContext := goReceiverContext(node, source)

		fn := goLowerFunction(node, source, limits)
		dataflow = append(dataflow, dataflowFunctionRow(name, line, classContext, fn))

		facts := goTaintFacts(node, source, fn)
		result := taint.Analyze(fn, facts, taint.DefaultLimits())
		for _, finding := range result.Findings {
			findings = append(findings, taintFindingRow(name, line, classContext, finding))
		}
	})
	sortFunctionRows(dataflow)
	sortFindingRows(findings)
	return dataflow, findings
}

// dataflowFunctionRow renders one function's CFG and def->use facts.
func dataflowFunctionRow(name string, line int, classContext string, fn cfg.Function) map[string]any {
	row := map[string]any{
		"name":        name,
		"line_number": line,
		"lang":        "go",
		"blocks":      dataflowBlockPayloads(fn.Blocks),
		"def_uses":    dataflowDefUsePayloads(fn.DefUses),
	}
	if classContext != "" {
		row["class_context"] = classContext
	}
	if fn.Overflow.Any() {
		row["overflow"] = map[string]any{
			"blocks":        fn.Overflow.Blocks,
			"stmts":         fn.Overflow.Stmts,
			"def_use_edges": fn.Overflow.DefUseEdges,
		}
	}
	return row
}

// taintFindingRow renders one taint finding with confidence and provenance.
func taintFindingRow(name string, line int, classContext string, finding taint.Finding) map[string]any {
	row := map[string]any{
		"function_name": name,
		"line_number":   line,
		"lang":          "go",
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
	if len(finding.Neutralized) > 0 {
		neutralized := make([]string, 0, len(finding.Neutralized))
		for _, kind := range finding.Neutralized {
			neutralized = append(neutralized, string(kind))
		}
		row["neutralized"] = neutralized
	}
	return row
}

// dataflowBlockPayloads renders basic blocks with their statements and sorted
// successors.
func dataflowBlockPayloads(blocks []cfg.Block) []map[string]any {
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

// dataflowDefUsePayloads renders resolved def->use edges in their already-sorted
// order.
func dataflowDefUsePayloads(defUses []cfg.DefUse) []map[string]any {
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

// sortFunctionRows orders dataflow rows by (line, name) for stable output.
func sortFunctionRows(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool {
		li, lj := intFromRow(rows[i], "line_number"), intFromRow(rows[j], "line_number")
		if li != lj {
			return li < lj
		}
		return stringFromRow(rows[i], "name") < stringFromRow(rows[j], "name")
	})
}

// sortFindingRows orders taint findings deterministically by sink line, source
// line, binding, and kind.
func sortFindingRows(rows []map[string]any) {
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
