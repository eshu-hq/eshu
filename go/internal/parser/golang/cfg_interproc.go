package golang

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/dataflowemit"
	"github.com/eshu-hq/eshu/go/internal/parser/interproc"
	"github.com/eshu-hq/eshu/go/internal/parser/summary"
	"github.com/eshu-hq/eshu/go/internal/parser/valueflow"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// goInterprocFindingPayloads derives a value-flow summary for every function in a
// file, composes them into an interprocedural port graph, and solves it,
// returning the cross-function taint findings. Resolution is intra-file (a callee
// is a function defined in the same file); cross-file and cross-repo composition
// is the reducer's job over the shared graph. The returned rows are deterministic
// (the solver sorts findings).
func goInterprocFindingPayloads(root *tree_sitter.Node, source []byte, importPath string) []map[string]any {
	localFuncs := goLocalFunctionIDs(root, source, importPath)
	summaries := map[summary.FunctionID]summary.Effects{}
	var sources []interproc.Source

	walkNamed(root, func(node *tree_sitter.Node) {
		switch node.Kind() {
		case "function_declaration", "method_declaration":
		default:
			return
		}
		name := strings.TrimSpace(nodeText(node.ChildByFieldName("name"), source))
		if name == "" {
			return
		}
		id := goFunctionID(importPath, goReceiverContext(node, source), name)
		fn := goLowerFunction(node, source, cfg.DefaultLimits())
		spec := goEffectsSpec(node, source, fn, localFuncs)
		summaries[id] = valueflow.DeriveEffects(fn, spec)
		sources = append(sources, goInterprocSources(node, source, id)...)
	})

	if len(sources) == 0 {
		return nil
	}
	program := valueflow.BuildProgram(summaries, sources, nil)
	result := interproc.SolvePartitioned(program, interproc.DefaultLimits())

	rows := make([]map[string]any, 0, len(result.Findings))
	for _, finding := range result.Findings {
		rows = append(rows, dataflowemit.InterprocFindingRow("go", finding))
	}
	return rows
}
