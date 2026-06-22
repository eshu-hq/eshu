package haskell

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// appendHaskellModuleBucket appends the module row the line-scan extractor
// emitted from the module header: the dotted name plus the header line span.
func appendHaskellModuleBucket(payload map[string]any, syntax haskellSyntaxIndex) {
	if syntax.module == nil {
		return
	}
	shared.AppendBucket(payload, "modules", map[string]any{
		"name":        syntax.module.name,
		"line_number": syntax.module.startLine,
		"end_line":    syntax.module.endLine,
		"lang":        "haskell",
	})
}

// appendHaskellClassBucket appends the classes-bucket rows for data, newtype,
// type-synonym, data-family, and typeclass declarations, annotating explicit
// module exports with the exported-type dead-code root.
func appendHaskellClassBucket(
	payload map[string]any,
	syntax haskellSyntaxIndex,
	explicitExports map[string]struct{},
) {
	for _, sym := range syntax.types {
		item := map[string]any{
			"name":          sym.name,
			"line_number":   sym.startLine,
			"end_line":      sym.endLine,
			"lang":          "haskell",
			"semantic_kind": sym.semanticKind,
		}
		if haskellIsExplicitExport(explicitExports, sym.name) {
			item["dead_code_root_kinds"] = []string{"haskell.exported_type"}
		}
		shared.AppendBucket(payload, "classes", item)
	}
}

// appendHaskellFunctionBuckets appends the functions-bucket rows for class and
// instance methods and for top-level value bindings, reproducing the line-scan
// payload: method rows carry their class or instance context and root kind;
// top-level rows carry main/module-export roots. A binding whose first line
// already contains the defining `=` stores that single line as source, while a
// guarded or multi-clause binding whose head line has no `=` stores its full
// node range and records is_dependency, matching the prior extractor.
func appendHaskellFunctionBuckets(
	payload map[string]any,
	syntax haskellSyntaxIndex,
	lines []string,
	explicitExports map[string]struct{},
	isDependency bool,
	options shared.Options,
	seenFunctions map[string]struct{},
) {
	for _, method := range syntax.methods {
		key := haskellFunctionKey(method.context, method.name)
		if _, ok := seenFunctions[key]; ok {
			continue
		}
		seenFunctions[key] = struct{}{}
		item := map[string]any{
			"name":                 method.name,
			"line_number":          method.startLine,
			"end_line":             method.endLine,
			"lang":                 "haskell",
			"class_context":        method.context,
			"decorators":           []string{},
			"dead_code_root_kinds": []string{method.rootKind},
		}
		if options.IndexSource && method.hasSource {
			item["source"] = method.source
		}
		shared.AppendBucket(payload, "functions", item)
	}

	for _, body := range syntax.classBodies {
		key := haskellFunctionKey(body.context, body.name)
		if _, ok := seenFunctions[key]; ok {
			continue
		}
		seenFunctions[key] = struct{}{}
		item := map[string]any{
			"name":                 body.name,
			"line_number":          body.startLine,
			"end_line":             body.endLine,
			"lang":                 "haskell",
			"class_context":        body.context,
			"decorators":           []string{},
			"dead_code_root_kinds": []string{body.rootKind},
		}
		if options.IndexSource {
			item["source"] = haskellNodeFirstLineSource(lines, body.startLine)
		}
		shared.AppendBucket(payload, "functions", item)
	}

	for _, value := range syntax.values {
		key := haskellFunctionKey("", value.name)
		if _, ok := seenFunctions[key]; ok {
			continue
		}
		seenFunctions[key] = struct{}{}
		_, rootKinds := haskellFunctionContextAndRoots(value.name, "", "", explicitExports)
		item := map[string]any{
			"name":        value.name,
			"line_number": value.startLine,
			"end_line":    value.endLine,
			"lang":        "haskell",
			"decorators":  []string{},
		}
		if len(rootKinds) > 0 {
			item["dead_code_root_kinds"] = rootKinds
		}
		if value.hasEqual {
			if options.IndexSource {
				item["source"] = value.firstLine
			}
		} else {
			item["is_dependency"] = isDependency
			if options.IndexSource {
				item["source"] = value.source
			}
		}
		shared.AppendBucket(payload, "functions", item)
	}
}

// appendHaskellValueCalls emits bounded lexical call evidence for each top-level
// value binding, each instance-method binding, and each typeclass default-method
// body across its line span. Each line is right-hand-side sliced by
// haskellAppendSpanCalls so the bound name is never reported as a call.
// Class-method type signatures contribute no call evidence because they have no
// binding body.
func appendHaskellValueCalls(
	payload map[string]any,
	syntax haskellSyntaxIndex,
	lines []string,
	seenCalls map[string]struct{},
) {
	for _, method := range syntax.methods {
		if !method.hasSource {
			continue
		}
		haskellAppendSpanCalls(
			payload,
			lines,
			method.startLine,
			method.endLine,
			method.name,
			method.context,
			method.params,
			seenCalls,
		)
	}
	for _, value := range syntax.values {
		haskellAppendSpanCalls(
			payload,
			lines,
			value.startLine,
			value.endLine,
			value.name,
			"",
			value.params,
			seenCalls,
		)
	}
	for _, body := range syntax.classBodies {
		haskellAppendSpanCalls(
			payload,
			lines,
			body.startLine,
			body.endLine,
			body.name,
			body.context,
			body.params,
			seenCalls,
		)
	}
}

// haskellAppendWhereVariables records simple local bindings from where blocks as
// variables. Where-block locals are scope-sensitive bindings the parser
// intentionally demotes out of the functions bucket: the tree-sitter grammar
// models them as nested function/bind nodes, but the package contract keeps only
// bare `name =`/`name` forms here and excludes parameterized locals such as
// `helper value = ...`. The bounded indentation scan over `haskellVariablePattern`
// stays a documented permanent exception rather than an AST symbol walk so the
// demotion contract and indentation sensitivity remain stable.
func haskellAppendWhereVariables(payload map[string]any, lines []string) {
	seenVariables := make(map[string]struct{})
	inWhereBlock := false
	for index := 0; index < len(lines); index++ {
		rawLine := lines[index]
		trimmed := strings.TrimSpace(rawLine)
		if inWhereBlock {
			if !strings.HasPrefix(rawLine, " ") && !strings.HasPrefix(rawLine, "\t") {
				inWhereBlock = false
			} else {
				if matches := haskellVariablePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
					name := matches[1]
					if _, ok := seenVariables[name]; !ok {
						seenVariables[name] = struct{}{}
						shared.AppendBucket(payload, "variables", map[string]any{
							"name":        name,
							"line_number": index + 1,
							"end_line":    index + 1,
							"lang":        "haskell",
						})
					}
				}
				continue
			}
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		if strings.HasSuffix(trimmed, "where") || trimmed == "where" {
			inWhereBlock = true
		}
	}
}

func haskellAppendSpanCalls(
	payload map[string]any,
	lines []string,
	startLine int,
	endLine int,
	functionName string,
	context string,
	params map[string]struct{},
	seenCalls map[string]struct{},
) {
	for lineNumber := startLine; lineNumber <= endLine && lineNumber <= len(lines); lineNumber++ {
		if lineNumber < 1 {
			continue
		}
		haskellAppendRHSCalls(
			payload,
			lines[lineNumber-1],
			lineNumber,
			functionName,
			context,
			params,
			seenCalls,
		)
	}
}
