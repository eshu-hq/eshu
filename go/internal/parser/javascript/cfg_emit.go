package javascript

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/cfg"
	"github.com/eshu-hq/eshu/go/internal/parser/dataflowemit"
	"github.com/eshu-hq/eshu/go/internal/parser/javascript/jsdataflow"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/parser/taint"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// emitValueFlowBuckets attaches the opt-in value-flow payload buckets
// (dataflow_functions, taint_findings, interproc_findings) when the gate is on.
// When off it is a no-op, so the payload stays byte-identical to before this
// feature. lang labels the rows (javascript, typescript, or tsx).
func emitValueFlowBuckets(payload map[string]any, root *tree_sitter.Node, source []byte, lang string, options shared.Options) {
	if !options.EmitDataflow {
		return
	}
	payload["dataflow_catalog_versions"] = []map[string]any{
		dataflowemit.CatalogVersionRow(lang, "taint", jsdataflow.TaintCatalogVersion()),
	}
	dataflow, findings := jsEmitDataflowBuckets(root, source, lang)
	if len(dataflow) > 0 {
		payload["dataflow_functions"] = dataflow
	}
	if len(findings) > 0 {
		payload["taint_findings"] = findings
	}
	if interprocRows := jsInterprocFindingPayloads(root, source, lang, options.RepositoryID); len(interprocRows) > 0 {
		payload["interproc_findings"] = interprocRows
	}
}

// jsEmitDataflowBuckets lowers every named function declaration and method in a
// file (intraprocedural taint is valid within any function body) and renders the
// "dataflow_functions" and "taint_findings" buckets. Both slices are sorted so
// the buckets are byte-stable across runs. A method carries its enclosing class
// name as class_context.
func jsEmitDataflowBuckets(root *tree_sitter.Node, source []byte, lang string) (dataflow, findings []map[string]any) {
	limits := cfg.DefaultLimits()
	var walk func(node *tree_sitter.Node, classContext string)
	walk = func(node *tree_sitter.Node, classContext string) {
		cursor := node.Walk()
		defer cursor.Close()
		for _, child := range node.NamedChildren(cursor) {
			child := child
			switch child.Kind() {
			case "function_declaration", "generator_function_declaration", "method_definition":
				nameNode := child.ChildByFieldName("name")
				name := strings.TrimSpace(nodeText(nameNode, source))
				if name != "" {
					line := nodeLine(nameNode)
					fn := jsdataflow.LowerFunction(&child, source, limits)
					dataflow = append(dataflow, dataflowemit.DataflowFunctionRow(lang, name, line, classContext, fn))
					facts := jsdataflow.TaintFacts(&child, source, fn)
					result := taint.Analyze(fn, facts, taint.DefaultLimits())
					for _, finding := range result.Findings {
						findings = append(findings, dataflowemit.TaintFindingRow(lang, name, line, classContext, finding))
					}
				}
				// Descend for nested functions, which carry no class context.
				walk(&child, "")
			case "class_declaration":
				className := strings.TrimSpace(nodeText(child.ChildByFieldName("name"), source))
				walk(&child, className)
			default:
				walk(&child, classContext)
			}
		}
	}
	walk(root, "")
	dataflowemit.SortFunctionRows(dataflow)
	dataflowemit.SortFindingRows(findings)
	return dataflow, findings
}

// jsInterprocFindingPayloads composes a file's per-function value-flow summaries
// into an interprocedural port graph and renders the cross-function taint
// findings. Resolution is intra-file; cross-file and cross-repo composition is
// the reducer's job. Import path is empty here until package ownership metadata
// is available for JS/TS, but repository identity is stable and durable.
func jsInterprocFindingPayloads(root *tree_sitter.Node, source []byte, lang string, repositoryID string) []map[string]any {
	findings := jsdataflow.InterprocFindings(root, source, repositoryID, "")
	if len(findings) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(findings))
	for _, finding := range findings {
		rows = append(rows, dataflowemit.InterprocFindingRow(lang, finding))
	}
	return rows
}
