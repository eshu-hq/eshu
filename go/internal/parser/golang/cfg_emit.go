package golang

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// goDataflowPayloads renders the per-function dataflow bucket using the default
// limits. It is the entry point used by the parser so language.go need not
// depend on the cfg package directly.
func goDataflowPayloads(root *tree_sitter.Node, source []byte) []map[string]any {
	return goDataflowFunctionPayloads(root, source, cfg.DefaultLimits())
}

// goDataflowFunctionPayloads lowers every top-level Go function and method to a
// control-flow graph, resolves reaching definitions, and renders the result as
// deterministic, bounded payload rows. Function literals are skipped here;
// closures are modeled by a later pass. The returned slice is sorted by
// (line, name) so the bucket is byte-stable across runs.
//
// limits bounds each function independently; a function that trips a cap still
// emits its blocks and records counted overflow rather than dropping data.
func goDataflowFunctionPayloads(root *tree_sitter.Node, source []byte, limits cfg.Limits) []map[string]any {
	var rows []map[string]any
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
		fn := goLowerFunction(node, source, limits)
		row := map[string]any{
			"name":        name,
			"line_number": nodeLine(nameNode),
			"lang":        "go",
			"blocks":      dataflowBlockPayloads(fn.Blocks),
			"def_uses":    dataflowDefUsePayloads(fn.DefUses),
		}
		if classContext := goReceiverContext(node, source); classContext != "" {
			row["class_context"] = classContext
		}
		if fn.Overflow.Any() {
			row["overflow"] = map[string]any{
				"blocks":        fn.Overflow.Blocks,
				"stmts":         fn.Overflow.Stmts,
				"def_use_edges": fn.Overflow.DefUseEdges,
			}
		}
		rows = append(rows, row)
	})
	sort.SliceStable(rows, func(i, j int) bool {
		li, lj := intFromRow(rows[i], "line_number"), intFromRow(rows[j], "line_number")
		if li != lj {
			return li < lj
		}
		ni, _ := rows[i]["name"].(string)
		nj, _ := rows[j]["name"].(string)
		return ni < nj
	})
	return rows
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

// intFromRow reads an int payload field, tolerating the int type emitted here.
func intFromRow(row map[string]any, key string) int {
	value, _ := row[key].(int)
	return value
}
