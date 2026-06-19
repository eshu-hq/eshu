package perl

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tree_sitter_perl "github.com/alexaandru/go-sitter-forest/perl"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Parse builds the parent parser payload for a Perl source file.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	parser := tree_sitter.NewParser()
	language := tree_sitter.NewLanguage(tree_sitter_perl.GetLanguage())
	if err := parser.SetLanguage(language); err != nil {
		parser.Close()
		return nil, fmt.Errorf("set parser language perl: %w", err)
	}
	defer parser.Close()

	return ParseWithParser(path, isDependency, options, parser)
}

// ParseWithParser builds the Perl payload with a caller-owned tree-sitter
// parser.
func ParseWithParser(path string, isDependency bool, options shared.Options, parser *tree_sitter.Parser) (map[string]any, error) {
	source, syntax, err := perlSourceAndSyntax(path, parser)
	if err != nil {
		return nil, err
	}
	payload := shared.BasePayload(path, "perl", isDependency)
	functionItems := make(map[string]map[string]any)
	for _, class := range syntax.classes {
		shared.AppendBucket(payload, "classes", class)
	}
	for _, imported := range syntax.imports {
		shared.AppendBucket(payload, "imports", imported)
	}
	for _, function := range syntax.functions {
		item := function.item
		if options.IndexSource && item["source"] == nil {
			item["source"] = perlLineRangeSource(source, shared.IntValue(item["line_number"]), shared.IntValue(item["end_line"]))
		}
		name, _ := item["name"].(string)
		functionItems[perlFunctionKey(function.packageName, name)] = item
		shared.AppendBucket(payload, "functions", item)
	}
	for packageName, exportedSubs := range syntax.exportsByPackage {
		perlRefreshExportedFunctionRoots(functionItems, packageName, exportedSubs)
	}
	for _, variable := range syntax.variables {
		shared.AppendBucket(payload, "variables", variable)
	}
	for _, call := range syntax.calls {
		shared.AppendBucket(payload, "function_calls", call)
	}

	shared.SortNamedBucket(payload, "functions")
	shared.SortNamedBucket(payload, "classes")
	shared.SortNamedBucket(payload, "variables")
	shared.SortNamedBucket(payload, "imports")
	shared.SortNamedBucket(payload, "function_calls")
	return payload, nil
}

func perlExportsForPackage(exportsByPackage map[string]map[string]struct{}, packageName string) map[string]struct{} {
	if exportsByPackage[packageName] == nil {
		exportsByPackage[packageName] = make(map[string]struct{})
	}
	return exportsByPackage[packageName]
}

func perlCollectExportNames(line string, exports map[string]struct{}) bool {
	segment, _, done := strings.Cut(line, ")")
	for _, field := range strings.Fields(segment) {
		name := strings.Trim(field, " \t\r\n,;")
		name = strings.TrimLeft(name, "$@%&")
		if name == "" || !isPerlIdentifier(name) {
			continue
		}
		exports[name] = struct{}{}
	}
	return done
}

func perlRefreshExportedFunctionRoots(functionItems map[string]map[string]any, packageName string, exportedSubs map[string]struct{}) {
	for name := range exportedSubs {
		item := functionItems[perlFunctionKey(packageName, name)]
		if item == nil {
			continue
		}
		addPerlRootKind(item, "perl.exported_subroutine")
	}
}

func perlFunctionKey(packageName string, name string) string {
	if packageName == "" {
		return name
	}
	return packageName + "::" + name
}

func perlFunctionRootKinds(name string, currentPackage string, exportedSubs map[string]struct{}, path string) []string {
	var kinds []string
	if name == "main" && isPerlScriptPath(path) {
		kinds = append(kinds, "perl.script_entrypoint")
	}
	if exportedSubs != nil {
		if _, ok := exportedSubs[name]; ok {
			kinds = append(kinds, "perl.exported_subroutine")
		}
	}
	if currentPackage != "" && name == "new" {
		kinds = append(kinds, "perl.constructor")
	}
	switch name {
	case "AUTOLOAD":
		kinds = append(kinds, "perl.autoload_subroutine")
	case "DESTROY":
		kinds = append(kinds, "perl.destroy_subroutine")
	}
	return kinds
}

func addPerlRootKind(item map[string]any, kinds ...string) {
	if len(kinds) == 0 {
		return
	}
	existing, _ := item["dead_code_root_kinds"].([]string)
	for _, kind := range kinds {
		if kind == "" || slices.Contains(existing, kind) {
			continue
		}
		existing = append(existing, kind)
	}
	if len(existing) > 0 {
		slices.Sort(existing)
		item["dead_code_root_kinds"] = existing
	}
}

func isPerlScriptPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".pl" || ext == ".t"
}

func perlIsPublicPackage(name string) bool {
	for _, part := range strings.Split(name, "::") {
		if part == "" || strings.HasPrefix(part, "_") {
			return false
		}
	}
	return true
}

func isPerlIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for index, char := range name {
		if index == 0 {
			if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && char != '_' {
				return false
			}
			continue
		}
		if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

// PreScan returns Perl subroutine and package names used by repository pre-scan.
func PreScan(path string) ([]string, error) {
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		return nil, err
	}
	names := shared.CollectBucketNames(payload, "functions", "classes")
	for _, bucket := range []string{"functions", "classes"} {
		items, _ := payload[bucket].([]map[string]any)
		for _, item := range items {
			fullName, _ := item["full_name"].(string)
			fullName = strings.TrimSpace(fullName)
			if fullName != "" {
				names = append(names, fullName)
			}
		}
	}
	return shared.DedupeNonEmptyStrings(names), nil
}
