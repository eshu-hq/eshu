// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scip

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	scippb "github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

// ParseResult captures the file payloads and symbol table derived from one index.scip file.
type ParseResult struct {
	Files       map[string]map[string]any
	SymbolTable map[string]map[string]any
}

// IndexParser parses one index.scip protobuf payload into Eshu file structures.
type IndexParser struct{}

// Parse reads one index.scip file and returns file payloads plus the enriched symbol table.
func (IndexParser) Parse(indexPath string, projectPath string) (ParseResult, error) {
	body, err := os.ReadFile(indexPath) // #nosec G304 -- reads an indexed SCIP file at a path derived from the parser's own scan target
	if err != nil {
		return ParseResult{}, fmt.Errorf("read SCIP index %q: %w", indexPath, err)
	}

	var index scippb.Index
	if err := proto.Unmarshal(body, &index); err != nil {
		return ParseResult{}, fmt.Errorf("parse SCIP index %q: %w", indexPath, err)
	}

	symbolTable := buildSymbolTable(&index)
	files, err := buildFiles(&index, projectPath, symbolTable)
	if err != nil {
		return ParseResult{}, err
	}
	return ParseResult{
		Files:       files,
		SymbolTable: symbolTable,
	}, nil
}

func buildSymbolTable(index *scippb.Index) map[string]map[string]any {
	table := make(map[string]map[string]any)
	for _, document := range index.GetDocuments() {
		for _, occurrence := range document.GetOccurrences() {
			if strings.HasPrefix(occurrence.GetSymbol(), "local ") {
				continue
			}
			if occurrence.GetSymbolRoles()&int32(scippb.SymbolRole_Definition) == 0 {
				continue
			}
			table[occurrence.GetSymbol()] = map[string]any{
				"file": document.GetRelativePath(),
				"line": occurrenceLine(occurrence),
			}
		}
		for _, symbol := range document.GetSymbols() {
			enrichSymbol(table, symbol)
		}
	}
	for _, symbol := range index.GetExternalSymbols() {
		enrichSymbol(table, symbol)
	}
	return table
}

func enrichSymbol(table map[string]map[string]any, symbol *scippb.SymbolInformation) {
	entry, ok := table[symbol.GetSymbol()]
	if !ok {
		return
	}
	entry["display_name"] = symbol.GetDisplayName()
	entry["documentation"] = strings.Join(symbol.GetDocumentation(), "\n")
	entry["kind"] = int32(symbol.GetKind())
}

func buildFiles(
	index *scippb.Index,
	projectPath string,
	symbolTable map[string]map[string]any,
) (map[string]map[string]any, error) {
	files := make(map[string]map[string]any, len(index.GetDocuments()))
	for _, document := range index.GetDocuments() {
		absolutePath, err := filepath.Abs(filepath.Join(projectPath, filepath.FromSlash(document.GetRelativePath())))
		if err != nil {
			return nil, fmt.Errorf("resolve SCIP document %q: %w", document.GetRelativePath(), err)
		}

		payload := map[string]any{
			"path":                absolutePath,
			"lang":                languageFromPath(document.GetRelativePath()),
			"is_dependency":       false,
			"functions":           []map[string]any{},
			"classes":             []map[string]any{},
			"variables":           []map[string]any{},
			"imports":             []map[string]any{},
			"function_calls_scip": []map[string]any{},
		}

		definitions := make([]*scippb.Occurrence, 0)
		for _, occurrence := range document.GetOccurrences() {
			if occurrence.GetSymbolRoles()&int32(scippb.SymbolRole_Definition) != 0 {
				definitions = append(definitions, occurrence)
			}
		}

		for _, occurrence := range document.GetOccurrences() {
			symbol := occurrence.GetSymbol()
			if strings.HasPrefix(symbol, "local ") {
				continue
			}
			if occurrence.GetSymbolRoles()&int32(scippb.SymbolRole_Definition) != 0 {
				appendDefinition(payload, symbol, occurrenceLine(occurrence), symbolTable[symbol])
				continue
			}
			appendReference(payload, symbol, occurrenceLine(occurrence), projectPath, symbolTable, definitions)
		}
		files[absolutePath] = payload
	}
	return files, nil
}

func appendDefinition(payload map[string]any, symbol string, line int, definition map[string]any) {
	kind := definitionKind(symbol, definition)
	name := nameFromSymbol(symbol)
	args, returnType := parseSignature(stringValueFromMap(definition, "display_name"))
	node := map[string]any{
		"name":          name,
		"line_number":   line,
		"end_line":      line,
		"scip_symbol":   symbol,
		"docstring":     stringValueFromMap(definition, "documentation"),
		"lang":          payload["lang"],
		"is_dependency": false,
		"return_type":   returnType,
		"args":          args,
	}

	switch kind {
	case int32(scippb.SymbolInformation_Function), int32(scippb.SymbolInformation_Method):
		// SCIP indexes carry symbols and signatures but no statement-level AST,
		// so real McCabe complexity cannot be computed here. Emit 0 (unknown)
		// instead of a fabricated 1: the native tree-sitter parser supplies the
		// true value for the same file, and complexity rankings exclude 0 rather
		// than surface a misleading constant. See issue #3488.
		node["cyclomatic_complexity"] = 0
		node["decorators"] = []string{}
		node["context"] = nil
		node["class_context"] = nil
		appendBucket(payload, "functions", node)
	case int32(scippb.SymbolInformation_Class):
		node["bases"] = []string{}
		node["context"] = nil
		appendBucket(payload, "classes", node)
	case int32(scippb.SymbolInformation_Field), int32(scippb.SymbolInformation_Variable):
		node["value"] = nil
		node["type"] = returnType
		node["context"] = nil
		node["class_context"] = nil
		appendBucket(payload, "variables", node)
	}
}

func appendReference(
	payload map[string]any,
	symbol string,
	line int,
	projectPath string,
	symbolTable map[string]map[string]any,
	definitions []*scippb.Occurrence,
) {
	calleeInfo, ok := symbolTable[symbol]
	if !ok {
		return
	}
	callerSymbol := findEnclosingDefinition(line, definitions)
	if callerSymbol == "" {
		return
	}
	callerInfo := symbolTable[callerSymbol]
	calleeFile, err := filepath.Abs(filepath.Join(projectPath, filepath.FromSlash(stringValueFromMap(calleeInfo, "file"))))
	if err != nil {
		return
	}

	edges, _ := payload["function_calls_scip"].([]map[string]any)
	payload["function_calls_scip"] = append(edges, map[string]any{
		"caller_symbol": callerSymbol,
		"caller_file":   payload["path"],
		"caller_line":   intValueFromMap(callerInfo, "line"),
		"callee_symbol": symbol,
		"callee_file":   calleeFile,
		"callee_line":   intValueFromMap(calleeInfo, "line"),
		"callee_name":   nameFromSymbol(symbol),
		"ref_line":      line,
	})
}

func definitionKind(symbol string, definition map[string]any) int32 {
	kind := int32(intValueFromMap(definition, "kind")) // #nosec G115 -- bounded: intValueFromMap returns a value sourced from protobuf int32 fields, always fits int32
	if kind != 0 {
		return kind
	}
	switch {
	case strings.Contains(symbol, "#") && strings.HasSuffix(symbol, "#"):
		return int32(scippb.SymbolInformation_Class)
	case strings.HasSuffix(symbol, "()."):
		return int32(scippb.SymbolInformation_Function)
	default:
		return 0
	}
}

func occurrenceLine(occurrence *scippb.Occurrence) int {
	if len(occurrence.GetRange()) == 0 {
		return 0
	}
	return int(occurrence.GetRange()[0]) + 1
}

var (
	trailingCallRe  = regexp.MustCompile(`\(\)\.?$`)
	separatorRe     = regexp.MustCompile(`[/#]`)
	signatureArgsRe = regexp.MustCompile(`\(([^)]*)\)`)
)

func nameFromSymbol(symbol string) string {
	stripped := strings.TrimRight(symbol, ".#")
	stripped = trailingCallRe.ReplaceAllString(stripped, "")
	parts := separatorRe.Split(stripped, -1)
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return symbol
	}
	return parts[len(parts)-1]
}

func languageFromPath(relativePath string) string {
	switch strings.ToLower(filepath.Ext(relativePath)) {
	case ".c":
		return "c"
	case ".cpp", ".h":
		return "cpp"
	case ".go":
		return "go"
	case ".java":
		return "java"
	case ".js", ".jsx":
		return "javascript"
	case ".py", ".ipynb":
		return "python"
	case ".rs":
		return "rust"
	case ".ts", ".tsx":
		return "typescript"
	default:
		return "unknown"
	}
}

func parseSignature(displayName string) ([]string, string) {
	args := make([]string, 0)
	if displayName == "" {
		return args, ""
	}

	returnType := ""
	if parts := strings.Split(displayName, "->"); len(parts) > 1 {
		returnType = strings.TrimSpace(parts[len(parts)-1])
	}
	matches := signatureArgsRe.FindStringSubmatch(displayName)
	if len(matches) != 2 || strings.TrimSpace(matches[1]) == "" {
		return args, returnType
	}
	for _, parameter := range strings.Split(matches[1], ",") {
		name := strings.TrimSpace(strings.Split(strings.Split(parameter, ":")[0], "=")[0])
		name = strings.TrimLeft(name, "*")
		if name != "" {
			args = append(args, name)
		}
	}
	return args, returnType
}

func findEnclosingDefinition(line int, definitions []*scippb.Occurrence) string {
	bestSymbol := ""
	bestLine := -1
	for _, occurrence := range definitions {
		defLine := occurrenceLine(occurrence)
		if defLine <= line && defLine > bestLine {
			bestLine = defLine
			bestSymbol = occurrence.GetSymbol()
		}
	}
	return bestSymbol
}

func stringValueFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	if value, ok := values[key].(string); ok {
		return value
	}
	return ""
}

func intValueFromMap(values map[string]any, key string) int {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
	case int:
		return value
	case int32:
		return int(value)
	default:
		return 0
	}
}
