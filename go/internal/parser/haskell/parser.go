package haskell

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_haskell "github.com/tree-sitter/tree-sitter-haskell/bindings/go"
)

// Permanent regex exceptions (epic #3531). These are bounded textual-evidence
// readers over scopes the tree-sitter walk already located, not primary symbol
// extraction, so they stay regex rather than AST node walks:
//
//   - haskellVariablePattern records simple where-block local bindings as
//     variables. The contract intentionally demotes where-locals out of the
//     functions bucket and keeps only bare `name =`/`name` forms; the bounded
//     indentation scan preserves that demotion and indentation sensitivity.
//   - haskellCallTokenPattern is the bounded lexical call-evidence token scan
//     over definition right-hand sides. Function-call rows are lexical evidence,
//     not compiler-resolved Haskell name binding, so they remain a token scan.
var (
	haskellVariablePattern  = regexp.MustCompile(`^\s*([a-z][A-Za-z0-9_']*)\s*(?:$|=)`)
	haskellCallTokenPattern = regexp.MustCompile(`(?:[A-Z][A-Za-z0-9_']*\.)+[a-z_][A-Za-z0-9_']*|[a-z_][A-Za-z0-9_']*`)
)

// Parse reads path and returns the legacy Haskell parser payload.
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

// ParseWithParser extracts Haskell payload data with a caller-owned tree-sitter parser.
func ParseWithParser(path string, isDependency bool, options shared.Options, parser *tree_sitter.Parser) (map[string]any, error) {
	source, syntax, err := haskellSourceAndSyntax(path, parser)
	if err != nil {
		return nil, err
	}

	payload := shared.BasePayload(path, "haskell", isDependency)
	payload["modules"] = []map[string]any{}
	lines := strings.Split(string(source), "\n")
	explicitExports := haskellExplicitExports(syntax)
	seenFunctions := make(map[string]struct{})
	seenCalls := make(map[string]struct{})

	appendHaskellModuleBucket(payload, syntax)
	haskellAppendImports(payload, lines)
	appendHaskellClassBucket(payload, syntax, explicitExports)
	appendHaskellFunctionBuckets(
		payload,
		syntax,
		lines,
		explicitExports,
		isDependency,
		options,
		seenFunctions,
	)
	haskellAppendWhereVariables(payload, lines)
	appendHaskellValueCalls(payload, syntax, lines, seenCalls)

	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "classes")
	shared.SortNamedBucket(payload, "modules")
	shared.SortNamedBucket(payload, "variables")
	shared.SortNamedBucket(payload, "imports")
	shared.SortNamedBucket(payload, "function_calls")
	return payload, nil
}

// haskellExplicitExports returns the explicit module-export names the tree-sitter
// header reader collected, or an empty set when the module omits an export list.
func haskellExplicitExports(syntax haskellSyntaxIndex) map[string]struct{} {
	if syntax.module == nil {
		return map[string]struct{}{}
	}
	return syntax.module.exports
}

// haskellAppendImports records import rows from the bounded import line helper.
// Imports are not tree-sitter symbol extraction: the helper normalizes safe,
// qualified, and package-qualified import forms and resolves common `as`
// aliases, so it stays a documented bounded-evidence reader rather than an AST
// symbol walk.
func haskellAppendImports(payload map[string]any, lines []string) {
	for index := 0; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		name, alias, ok := haskellParseImport(trimmed)
		if !ok {
			continue
		}
		item := map[string]any{
			"name":        name,
			"line_number": index + 1,
			"lang":        "haskell",
		}
		if alias != "" {
			item["alias"] = alias
		}
		shared.AppendBucket(payload, "imports", item)
	}
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
