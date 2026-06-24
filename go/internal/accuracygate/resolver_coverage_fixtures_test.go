// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package accuracygate_test

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// resolverCoverageFixture drives one documented resolver language through the
// real reducer call-row extraction. The envelopes are the minimal raw
// `function_calls` + `imports` + repository `imports_map` shape that makes that
// language's dedicated resolver fire, mirroring the per-language reducer resolver
// tests. callerUID/calleeUID name the edge the resolver must produce, and
// resolutionMethod is the production resolution provenance the row must carry, so
// a same-name repo fallback never counts as resolver coverage.
type resolverCoverageFixture struct {
	language         string
	envelopes        []facts.Envelope
	callerUID        string
	calleeUID        string
	resolutionMethod string
}

const (
	methodImportBinding = "import_binding"
	methodTypeInferred  = "type_inferred"
)

// resolverCoverageFixtures returns one fixture per documented resolver language
// from the #3487 matrix, including the jsx and tsx aliases that share the
// javascript and typescript resolvers. Each fixture exercises the real resolver
// and is expected to produce exactly the named CALLS edge with the named
// resolution method. Removing any one resolver makes its fixture stop producing
// the edge, dropping the measured covered count below the gate floor.
func resolverCoverageFixtures() []resolverCoverageFixture {
	fixtures := []resolverCoverageFixture{
		goResolverCoverageFixture(),
		pythonResolverCoverageFixture(),
		typescriptResolverCoverageFixture("typescript"),
		typescriptResolverCoverageFixture("tsx"),
		javascriptResolverCoverageFixture("javascript"),
		javascriptResolverCoverageFixture("jsx"),
		swiftResolverCoverageFixture(),
		javaResolverCoverageFixture(),
		kotlinResolverCoverageFixture(),
		dartResolverCoverageFixture(),
		elixirResolverCoverageFixture(),
		groovyResolverCoverageFixture(),
		haskellResolverCoverageFixture(),
		perlResolverCoverageFixture(),
		rustResolverCoverageFixture(),
	}
	return fixtures
}

// goResolverCoverageFixture exercises the Go same-directory resolver: a caller and
// a uniquely named callee in two files in the same package directory bind by
// scope-unique name, the provenance the Go same-directory resolver records.
func goResolverCoverageFixture() resolverCoverageFixture {
	return resolverCoverageFixture{
		language:         "go",
		callerUID:        "uid:go-caller",
		calleeUID:        "uid:go-callee",
		resolutionMethod: "scope_unique_name",
		envelopes: []facts.Envelope{
			{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-go"}},
			fileEnvelope(
				"repo-go", "pkg/caller.go", "pkg/caller.go",
				[]any{fnLang("Caller", "uid:go-caller", 3, 5, "go")},
				nil,
				[]any{call(map[string]any{"name": "callee", "full_name": "callee", "line_number": 4, "lang": "go"})},
			),
			fileEnvelope(
				"repo-go", "pkg/callee.go", "pkg/callee.go",
				[]any{fnLang("callee", "uid:go-callee", 1, 2, "go")},
				nil,
				nil,
			),
		},
	}
}

// pythonResolverCoverageFixture exercises the Python declared-class-method
// resolver: a qualified Service.run() call binds to the unique Service.run method
// declaration by type inference, the provenance the Python resolver records.
func pythonResolverCoverageFixture() resolverCoverageFixture {
	return resolverCoverageFixture{
		language:         "python",
		callerUID:        "uid:py-caller",
		calleeUID:        "uid:py-run",
		resolutionMethod: methodTypeInferred,
		envelopes: []facts.Envelope{
			{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-python"}},
			fileEnvelope(
				"repo-python", "pkg/caller.py", "pkg/caller.py",
				[]any{fnLang("caller", "uid:py-caller", 4, 6, "python")},
				nil,
				[]any{call(map[string]any{"name": "run", "full_name": "Service.run", "line_number": 5, "lang": "python"})},
			),
			fileEnvelopeWithClasses(
				"repo-python", "pkg/service.py", "pkg/service.py",
				[]any{pyMethod("run", "uid:py-run", "Service", 2, 4)},
				[]any{map[string]any{"name": "Service", "uid": "uid:py-service", "lang": "python", "line_number": 1, "end_line": 4}},
				nil,
			),
		},
	}
}

// typescriptResolverCoverageFixture exercises the TypeScript interface-implementer
// receiver-type resolver under the given language token (typescript or tsx). The
// implementing class FetchTransport declares it implements the Transport
// interface, and FetchTransport.request is the unique method, so a call on a
// Transport-typed receiver binds to it by type inference.
func typescriptResolverCoverageFixture(lang string) resolverCoverageFixture {
	callerUID := "uid:" + lang + "-caller"
	calleeUID := "uid:" + lang + "-impl"
	repoID := "repo-" + lang
	calleeFile := facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": "src/transport." + tsExt(lang),
			"parsed_file_data": map[string]any{
				"path":       "src/transport." + tsExt(lang),
				"functions":  []any{fnImpl("request", calleeUID, "FetchTransport", lang, 6, 8)},
				"interfaces": []any{iface("Transport", "uid:"+lang+"-iface", lang, 1, 3)},
				"classes": []any{map[string]any{
					"name":                   "FetchTransport",
					"uid":                    "uid:" + lang + "-class",
					"lang":                   lang,
					"implemented_interfaces": []any{"Transport"},
					"line_number":            5,
					"end_line":               9,
				}},
			},
		},
	}
	return resolverCoverageFixture{
		language:         lang,
		callerUID:        callerUID,
		calleeUID:        calleeUID,
		resolutionMethod: methodTypeInferred,
		envelopes: []facts.Envelope{
			{FactKind: "repository", Payload: map[string]any{"repo_id": repoID}},
			fileEnvelope(
				repoID, "src/caller."+tsExt(lang), "src/caller."+tsExt(lang),
				[]any{fnLang("run", callerUID, 1, 4, lang)},
				nil,
				[]any{call(map[string]any{
					"name":              "request",
					"full_name":         "transport.request",
					"inferred_obj_type": "Transport",
					"line_number":       3,
					"lang":              lang,
				})},
			),
			calleeFile,
		},
	}
}

func tsExt(lang string) string {
	if lang == "tsx" {
		return "tsx"
	}
	return "ts"
}

// javascriptResolverCoverageFixture exercises the JavaScript receiver-type method
// resolver under the given language token (javascript or jsx).
func javascriptResolverCoverageFixture(lang string) resolverCoverageFixture {
	callerUID := "uid:" + lang + "-caller"
	calleeUID := "uid:" + lang + "-invoke"
	repoID := "repo-" + lang
	ext := "js"
	if lang == "jsx" {
		ext = "jsx"
	}
	return resolverCoverageFixture{
		language:         lang,
		callerUID:        callerUID,
		calleeUID:        calleeUID,
		resolutionMethod: methodTypeInferred,
		envelopes: []facts.Envelope{
			{FactKind: "repository", Payload: map[string]any{"repo_id": repoID}},
			fileEnvelope(
				repoID, "src/caller."+ext, "src/caller."+ext,
				[]any{fnLang("run", callerUID, 1, 5, lang)},
				nil,
				[]any{call(map[string]any{
					"name":              "invoke",
					"full_name":         "worker.invoke",
					"inferred_obj_type": "Worker",
					"line_number":       3,
					"lang":              lang,
				})},
			),
			fileEnvelope(
				repoID, "src/worker."+ext, "src/worker."+ext,
				[]any{fnImpl("invoke", calleeUID, "Worker", lang, 2, 4)},
				nil,
				nil,
			),
		},
	}
}

// swiftResolverCoverageFixture exercises Swift receiver-type method inference.
func swiftResolverCoverageFixture() resolverCoverageFixture {
	return resolverCoverageFixture{
		language:         "swift",
		callerUID:        "uid:swift-caller",
		calleeUID:        "uid:swift-info",
		resolutionMethod: methodTypeInferred,
		envelopes: []facts.Envelope{
			{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-swift"}},
			fileEnvelope(
				"repo-swift", "Sources/Caller.swift", "Sources/Caller.swift",
				[]any{fn("run", "uid:swift-caller", 1, 5)},
				nil,
				[]any{call(map[string]any{
					"name":              "info",
					"full_name":         "logger.info",
					"inferred_obj_type": "Logger",
					"line_number":       3,
					"lang":              "swift",
				})},
			),
			fileEnvelope(
				"repo-swift", "Sources/Logger.swift", "Sources/Logger.swift",
				[]any{fnImpl("info", "uid:swift-info", "Logger", "swift", 2, 4)},
				nil,
				nil,
			),
		},
	}
}

// javaResolverCoverageFixture exercises the Java receiver-type method resolver.
func javaResolverCoverageFixture() resolverCoverageFixture {
	return resolverCoverageFixture{
		language:         "java",
		callerUID:        "uid:java-caller",
		calleeUID:        "uid:java-process",
		resolutionMethod: methodTypeInferred,
		envelopes: []facts.Envelope{
			{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-java"}},
			fileEnvelope(
				"repo-java", "src/main/java/example/Worker.java", "src/main/java/example/Worker.java",
				[]any{fn("run", "uid:java-caller", 1, 5)},
				nil,
				[]any{call(map[string]any{
					"name":              "process",
					"full_name":         "process",
					"inferred_obj_type": "Service",
					"argument_types":    []any{"Task"},
					"line_number":       3,
					"lang":              "java",
				})},
			),
			fileEnvelope(
				"repo-java", "src/main/java/example/Service.java", "src/main/java/example/Service.java",
				[]any{fnClassParams("process", "uid:java-process", "Service", []any{"Task"}, 1, 3)},
				nil,
				nil,
			),
		},
	}
}

// kotlinResolverCoverageFixture exercises the Kotlin imported-receiver binding
// resolver: an imported Service type binds the call to the imported file.
func kotlinResolverCoverageFixture() resolverCoverageFixture {
	return resolverCoverageFixture{
		language:         "kotlin",
		callerUID:        "uid:kotlin-caller",
		calleeUID:        "uid:kotlin-query",
		resolutionMethod: methodImportBinding,
		envelopes: []facts.Envelope{
			{FactKind: "repository", Payload: map[string]any{
				"repo_id": "repo-kotlin",
				"imports_map": map[string][]string{
					"Service": {"src/main/kotlin/com/example/lib/Service.kt"},
				},
			}},
			fileEnvelopeWithImports(
				"repo-kotlin", "src/main/kotlin/example/Caller.kt", "src/main/kotlin/example/Caller.kt",
				[]any{fn("run", "uid:kotlin-caller", 4, 7)},
				[]any{map[string]any{
					"name":        "com.example.lib.Service",
					"alias":       "Service",
					"source":      "com.example.lib.Service",
					"import_type": "import",
					"lang":        "kotlin",
				}},
				[]any{call(map[string]any{
					"name":              "query",
					"full_name":         "service.query",
					"inferred_obj_type": "Service",
					"argument_types":    []any{"Task"},
					"argument_count":    1,
					"line_number":       5,
					"lang":              "kotlin",
				})},
			),
			fileEnvelope(
				"repo-kotlin", "src/main/kotlin/com/example/lib/Service.kt", "src/main/kotlin/com/example/lib/Service.kt",
				[]any{fnClassParams("query", "uid:kotlin-query", "Service", []any{"Task"}, 2, 4)},
				nil,
				nil,
			),
		},
	}
}

// fileEnvelope builds a "file" fact envelope with functions and function_calls.
func fileEnvelope(repoID, relativePath, path string, functions, imports, calls []any) facts.Envelope {
	return fileEnvelopeWithInterfaces(repoID, relativePath, path, functions, nil, imports, calls)
}

// fileEnvelopeWithImports builds a "file" fact envelope carrying imports.
func fileEnvelopeWithImports(repoID, relativePath, path string, functions, imports, calls []any) facts.Envelope {
	return fileEnvelopeWithInterfaces(repoID, relativePath, path, functions, nil, imports, calls)
}

// fileEnvelopeWithInterfaces builds a "file" fact envelope carrying optional
// interfaces (used by the TypeScript interface-implementer resolver).
func fileEnvelopeWithInterfaces(repoID, relativePath, path string, functions, interfaces, imports, calls []any) facts.Envelope {
	parsed := map[string]any{"path": path}
	if len(functions) > 0 {
		parsed["functions"] = functions
	}
	if len(interfaces) > 0 {
		parsed["interfaces"] = interfaces
	}
	if len(imports) > 0 {
		parsed["imports"] = imports
	}
	if len(calls) > 0 {
		parsed["function_calls"] = calls
	}
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":          repoID,
			"relative_path":    relativePath,
			"parsed_file_data": parsed,
		},
	}
}

// fileEnvelopeWithClasses builds a "file" fact envelope carrying class
// declarations (used by the Python declared-class-method resolver).
func fileEnvelopeWithClasses(repoID, relativePath, path string, functions, classes, calls []any) facts.Envelope {
	envelope := fileEnvelope(repoID, relativePath, path, functions, nil, calls)
	parsed := envelope.Payload["parsed_file_data"].(map[string]any)
	if len(classes) > 0 {
		parsed["classes"] = classes
	}
	return envelope
}

// pyMethod builds a Python method declaration function item carrying its class
// context and language so the declared-class-method index keys it by Class.method.
func pyMethod(name, uid, classContext string, line, endLine int) map[string]any {
	item := fnClass(name, uid, classContext, line, endLine)
	item["lang"] = "python"
	return item
}

// call returns a function_calls entry from the given fields.
func call(fields map[string]any) map[string]any { return fields }

// fn builds a function item with a uid and line range.
func fn(name, uid string, line, endLine int) map[string]any {
	return map[string]any{"name": name, "uid": uid, "line_number": line, "end_line": endLine}
}

// fnLang builds a function item carrying an explicit language token.
func fnLang(name, uid string, line, endLine int, lang string) map[string]any {
	item := fn(name, uid, line, endLine)
	item["lang"] = lang
	return item
}

// fnClass builds a function item with a class_context.
func fnClass(name, uid, classContext string, line, endLine int) map[string]any {
	item := fn(name, uid, line, endLine)
	item["class_context"] = classContext
	return item
}

// fnClassParams builds a function item with a class_context and parameter types.
func fnClassParams(name, uid, classContext string, paramTypes []any, line, endLine int) map[string]any {
	item := fnClass(name, uid, classContext, line, endLine)
	item["parameter_types"] = paramTypes
	return item
}

// fnImpl builds a function item that implements a class/interface in a language.
func fnImpl(name, uid, classContext, lang string, line, endLine int) map[string]any {
	item := fnClass(name, uid, classContext, line, endLine)
	item["lang"] = lang
	return item
}

// iface builds an interface item for the TypeScript interface-implementer
// resolver.
func iface(name, uid, lang string, line, endLine int) map[string]any {
	return map[string]any{"name": name, "uid": uid, "lang": lang, "line_number": line, "end_line": endLine}
}
