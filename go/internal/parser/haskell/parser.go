package haskell

import (
	"fmt"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_haskell "github.com/tree-sitter/tree-sitter-haskell/bindings/go"
)

// Parse reads path and returns the Haskell parser payload.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	parser := tree_sitter.NewParser()
	language := tree_sitter.NewLanguage(tree_sitter_haskell.Language())
	if err := parser.SetLanguage(language); err != nil {
		parser.Close()
		return nil, fmt.Errorf("set parser language haskell: %w", err)
	}
	defer parser.Close()

	return ParseWithParser(path, isDependency, options, parser)
}

// ParseWithParser extracts Haskell payload data with a caller-owned tree-sitter
// parser. Symbol extraction is driven by a single walk of the parse tree; the
// caller retains ownership of parser and must not have it closed here.
func ParseWithParser(path string, isDependency bool, options shared.Options, parser *tree_sitter.Parser) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	if parser == nil {
		return nil, fmt.Errorf("parse haskell tree: nil parser")
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse haskell tree: parser returned nil tree")
	}
	defer tree.Close()

	payload := shared.BasePayload(path, "haskell", isDependency)
	payload["modules"] = []map[string]any{}
	lines := strings.Split(string(source), "\n")

	extractor := newHaskellExtractor(payload, source, lines, isDependency, options)
	extractor.extract(tree.RootNode())

	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "classes")
	shared.SortNamedBucket(payload, "modules")
	shared.SortNamedBucket(payload, "variables")
	shared.SortNamedBucket(payload, "imports")
	shared.SortNamedBucket(payload, "function_calls")
	return payload, nil
}

// PreScan returns Haskell declaration names used by repository pre-scan.
func PreScan(path string) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes", "modules")
	slices.Sort(names)
	return names, nil
}

// PreScanWithParser returns Haskell pre-scan names with a caller-owned tree-sitter parser.
func PreScanWithParser(path string, parser *tree_sitter.Parser) ([]string, error) {
	payload, err := ParseWithParser(path, false, shared.Options{}, parser)
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes", "modules")
	slices.Sort(names)
	return names, nil
}
