package elixir

import (
	"fmt"
	"slices"

	tree_sitter_elixir "github.com/tree-sitter/tree-sitter-elixir/bindings/go"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Parse extracts Elixir modules, functions, imports, attributes, and calls.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	parser := tree_sitter.NewParser()
	language := tree_sitter.NewLanguage(tree_sitter_elixir.Language())
	if err := parser.SetLanguage(language); err != nil {
		parser.Close()
		return nil, fmt.Errorf("set parser language elixir: %w", err)
	}
	defer parser.Close()

	return ParseWithParser(path, isDependency, options, parser)
}

// ParseWithParser extracts Elixir payload data with a caller-owned tree-sitter parser.
func ParseWithParser(path string, isDependency bool, options shared.Options, parser *tree_sitter.Parser) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	if parser == nil {
		return nil, fmt.Errorf("parse elixir tree: nil parser")
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse elixir tree: parser returned nil tree")
	}
	defer tree.Close()

	payload := shared.BasePayload(path, "elixir", isDependency)
	payload["modules"] = []map[string]any{}
	payload["protocols"] = []map[string]any{}
	appendHexDependencyRows(payload, path, string(source))

	extractor := newElixirExtractor(payload, source, isDependency, options)
	extractor.extract(tree.RootNode())

	for _, bucket := range []string{"functions", "modules", "protocols", "variables", "imports", "function_calls"} {
		shared.SortNamedBucket(payload, bucket)
	}
	return payload, nil
}

// PreScan returns Elixir names used by the collector import-map pre-scan.
func PreScan(path string) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "modules", "protocols")
	slices.Sort(names)
	return names, nil
}

// PreScanWithParser returns Elixir pre-scan names with a caller-owned tree-sitter parser.
func PreScanWithParser(path string, parser *tree_sitter.Parser) ([]string, error) {
	payload, err := ParseWithParser(path, false, shared.Options{}, parser)
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "modules", "protocols")
	slices.Sort(names)
	return names, nil
}
