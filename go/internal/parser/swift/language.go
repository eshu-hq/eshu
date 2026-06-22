package swift

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Parse extracts Swift imports, types, functions, variables, and calls. The
// tree-sitter AST node walk is the sole extraction path: a first pass records
// whole-file semantic facts (conformances, protocol methods, Vapor route
// handlers) and a second pass emits every payload row from AST nodes.
func Parse(path string, isDependency bool, options shared.Options, parser *tree_sitter.Parser) (map[string]any, error) {
	source, tree, syntax, err := swiftSourceAndSyntax(path, parser)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	payload := shared.BasePayload(path, "swift", isDependency)
	payload["structs"] = []map[string]any{}
	payload["enums"] = []map[string]any{}
	payload["protocols"] = []map[string]any{}

	builder := newSwiftPayloadBuilder(payload, syntax.facts(), isDependency, options.IndexSource)
	builder.emit(tree.RootNode(), source, nil)

	for _, bucket := range []string{"functions", "classes", "structs", "enums", "protocols", "variables", "imports", "function_calls"} {
		shared.SortNamedBucket(payload, bucket)
	}

	return payload, nil
}

// PreScan returns Swift names used by the collector import-map pre-scan.
func PreScan(path string, parser *tree_sitter.Parser) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{}, parser)
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes", "structs", "enums", "protocols")
	slices.Sort(names)
	return names, nil
}
