// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

func resolveSameFileCalleeEntityID(
	index codeEntityIndex,
	rawPath string,
	relativePath string,
	call map[string]any,
) string {
	language := codeCallLanguage(call, rawPath, relativePath)
	for _, name := range codeCallExactCandidateNames(call, language) {
		for _, pathKey := range codeCallPathKeys(rawPath, relativePath) {
			if entityID := index.uniqueNameByPath[pathKey][name]; entityID != "" {
				return entityID
			}
		}
	}
	if codeCallHasQualifiedScope(call, language) {
		return ""
	}
	for _, name := range codeCallBroadCandidateNames(call, language) {
		for _, pathKey := range codeCallPathKeys(rawPath, relativePath) {
			if entityID := index.uniqueNameByPath[pathKey][name]; entityID != "" {
				return entityID
			}
		}
	}
	return ""
}

func codeCallExactCandidateNames(call map[string]any, language string) []string {
	names := make([]string, 0, 6)
	appendName := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		for _, existing := range names {
			if existing == trimmed {
				return
			}
		}
		names = append(names, trimmed)
	}

	name := anyToString(call["name"])
	fullName := anyToString(call["full_name"])
	if codeCallHasQualifiedFullName(fullName) {
		appendName(fullName)
		if codeCallJavaClassReferenceKind(call) {
			appendName(name)
		}
		if language == "python" && codeCallPythonQualifiedClassReceiver(fullName) {
			appendName(codeCallTrailingName(fullName))
		}
		if codeCallJavaScriptFamily(language) && strings.HasPrefix(fullName, "module.exports.") {
			appendName(codeCallTrailingName(fullName))
		}
		if codeCallJavaScriptFamily(language) {
			for _, receiver := range codeCallJavaScriptFunctionReceiverNames(fullName) {
				appendName(receiver)
				if strings.HasPrefix(receiver, "module.exports.") {
					appendName(codeCallTrailingName(receiver))
				}
			}
		}
		if language == "dart" {
			// Fail open (#5332 regression): the Dart AST rewrite emits a
			// receiver-qualified full_name ("repository.create") for BOTH
			// class/static/named-constructor references and ordinary
			// instance-variable receivers — unlike Python, which only adds a
			// bare fallback for class-style receivers
			// (codeCallPythonQualifiedClassReceiver). A variable receiver's
			// qualified full_name never matches a real declaration (the
			// receiver is a local variable, not a resolvable scope), so
			// without a bare fallback the call is silently unresolved and the
			// CALLS edge is dropped. Dart therefore always appends the bare
			// trailing name behind the qualified primary, regardless of how
			// codeCallDartQualifiedClassReceiver classifies the receiver:
			// for a class receiver it's a harmless last resort behind the
			// already-tried qualified match; for a variable receiver it's the
			// only way the call resolves at all, restoring the byte-scanner
			// fallback behavior that predates the AST rewrite. This cannot
			// mis-bind a bare name with multiple same-named declarations
			// across the repo: index construction only keeps a name in
			// uniqueNameByRepo when exactly one declaration claims it.
			appendName(codeCallTrailingName(fullName))
		}
	}
	for _, classContext := range codeCallClassContexts(call) {
		appendName(classContext + "." + name)
	}
	inferredType := strings.TrimSpace(anyToString(call["inferred_obj_type"]))
	if inferredType != "" && strings.TrimSpace(name) != "" {
		appendName(inferredType + "." + name)
		if language == "php" && strings.Contains(inferredType, "\\") {
			appendName(codeCallTrailingName(inferredType) + "." + name)
		}
	}
	contextName := codeCallContextName(call["context"])
	contextType := codeCallContextType(call)
	if language == "ruby" &&
		contextName != "" &&
		(contextType == "class" || contextType == "module") &&
		strings.TrimSpace(name) != "" {
		appendName(contextName + "." + name)
	}
	if arity, ok := codeCallMetadataInt(call, "argument_count"); ok {
		names = codeCallAppendArityNames(names, arity)
	}
	if argumentTypes := codeCallMetadataStringSlice(call, "argument_types"); len(argumentTypes) > 0 {
		names = codeCallAppendTypedSignatureNames(names, argumentTypes)
	}
	return names
}

func codeCallJavaClassReferenceKind(call map[string]any) bool {
	switch strings.TrimSpace(anyToString(call["call_kind"])) {
	case "java.reflection_class_reference", "java.service_loader_provider", "java.spring_autoconfiguration_class":
		return true
	default:
		return false
	}
}

func codeCallPythonQualifiedClassReceiver(fullName string) bool {
	trimmed := strings.TrimSpace(fullName)
	dot := strings.LastIndex(trimmed, ".")
	if dot <= 0 || dot >= len(trimmed)-1 {
		return false
	}
	receiver := codeCallTrailingName(trimmed[:dot])
	if receiver == "" {
		return false
	}
	first := rune(receiver[0])
	return first >= 'A' && first <= 'Z'
}

// codeCallDartQualifiedClassReceiver mirrors
// codeCallPythonQualifiedClassReceiver's structure: it reports whether a
// Dart qualified full_name's receiver segment (the text before the final
// delimiter, with a leading "_" or "$" stripped) is UpperCamelCase and
// therefore names a class, static member, or named constructor rather than
// an instance-variable, keyword ("super"/"this"), multi-segment, or
// otherwise unrecognized receiver. Unlike the Python helper, this
// classification does not gate whether codeCallExactCandidateNames appends
// the bare fallback for Dart — that append is unconditional (fail open, see
// the "dart" branch above) because a variable receiver has no other path to
// resolution. The classifier exists to make that distinction explainable and
// independently testable.
func codeCallDartQualifiedClassReceiver(fullName string) bool {
	trimmed := strings.TrimSpace(fullName)
	dot := strings.LastIndex(trimmed, ".")
	if dot <= 0 || dot >= len(trimmed)-1 {
		return false
	}
	receiver := strings.TrimLeft(codeCallTrailingName(trimmed[:dot]), "_$")
	if receiver == "" {
		return false
	}
	first := rune(receiver[0])
	return first >= 'A' && first <= 'Z'
}

func codeCallJavaScriptFunctionReceiverNames(fullName string) []string {
	fullName = strings.TrimSpace(fullName)
	for _, method := range []string{".call", ".apply", ".bind"} {
		if receiver, ok := strings.CutSuffix(fullName, method); ok && strings.TrimSpace(receiver) != "" {
			return []string{receiver}
		}
	}
	return nil
}

func codeCallJavaScriptFamily(language string) bool {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "javascript", "jsx", "typescript", "tsx":
		return true
	default:
		return false
	}
}

func codeCallBroadCandidateNames(call map[string]any, language string) []string {
	if language == "elixir" {
		return nil
	}

	names := make([]string, 0, 4)
	appendName := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		for _, existing := range names {
			if existing == trimmed {
				return
			}
		}
		names = append(names, trimmed)
	}

	name := anyToString(call["name"])
	fullName := anyToString(call["full_name"])
	appendName(name)
	appendName(fullName)
	appendName(codeCallTrailingName(fullName))
	appendName(codeCallTrailingSegments(fullName, 2))
	if arity, ok := codeCallMetadataInt(call, "argument_count"); ok {
		names = codeCallAppendArityNames(names, arity)
	}
	if argumentTypes := codeCallMetadataStringSlice(call, "argument_types"); len(argumentTypes) > 0 {
		names = codeCallAppendTypedSignatureNames(names, argumentTypes)
	}
	return names
}

func codeCallTrailingName(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	cutset := func(r rune) bool {
		switch r {
		case '.', ':', '#', '/', '\\':
			return true
		default:
			return false
		}
	}
	parts := strings.FieldsFunc(trimmed, cutset)
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func codeCallPreferredPath(rawPath string, relativePath string) string {
	if normalized := normalizeCodeCallPath(relativePath); normalized != "" {
		return normalized
	}
	return normalizeCodeCallPath(rawPath)
}

func codeCallFunctionCandidateNames(item map[string]any) []string {
	names := make([]string, 0, 5)
	appendName := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		for _, existing := range names {
			if existing == trimmed {
				return
			}
		}
		names = append(names, trimmed)
	}

	name := anyToString(item["name"])
	appendName(name)
	fullName := anyToString(item["full_name"])
	appendName(fullName)
	if implContext := codeCallImplContext(item); implContext != "" && name != "" {
		appendName(implContext + "::" + name)
	}
	classContext := codeCallClassContext(item["class_context"])
	if classContext != "" && strings.TrimSpace(name) != "" {
		appendName(classContext + "." + name)
	}
	contextName := codeCallContextName(item["context"])
	contextType := codeCallContextType(item)
	if contextName != "" &&
		(contextType == "class" || contextType == "module") &&
		strings.TrimSpace(name) != "" {
		appendName(contextName + "." + name)
	}
	if arity, ok := codeCallMetadataInt(item, "parameter_count"); ok {
		names = codeCallAppendArityNames(names, arity)
	}
	if parameterTypes := codeCallMetadataStringSlice(item, "parameter_types"); len(parameterTypes) > 0 {
		names = codeCallAppendTypedSignatureNames(names, parameterTypes)
	}
	return names
}

func codeCallTypeCandidateNames(item map[string]any) []string {
	names := make([]string, 0, 3)
	appendName := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		for _, existing := range names {
			if existing == trimmed {
				return
			}
		}
		names = append(names, trimmed)
	}

	appendName(anyToString(item["name"]))
	appendName(anyToString(item["full_name"]))
	appendName(codeCallContextName(item["context"]))
	return names
}

func codeCallImplContext(item map[string]any) string {
	switch typed := item["impl_context"].(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return ""
	}
}

func codeCallClassContext(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		if len(typed) == 0 {
			return ""
		}
		return strings.TrimSpace(anyToString(typed[0]))
	default:
		return ""
	}
}

// codeCallClassContexts preserves nearest-to-outermost class scopes emitted by
// language parsers, allowing exact same-file matching without broad fallback.
func codeCallClassContexts(item map[string]any) []string {
	contexts := make([]string, 0, 4)
	appendContext := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		for _, existing := range contexts {
			if existing == trimmed {
				return
			}
		}
		contexts = append(contexts, trimmed)
	}

	appendContext(codeCallClassContext(item["class_context"]))
	switch typed := item["enclosing_class_contexts"].(type) {
	case []string:
		for _, value := range typed {
			appendContext(value)
		}
	case []any:
		for _, value := range typed {
			appendContext(anyToString(value))
		}
	}
	return contexts
}

func codeCallContextName(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		if len(typed) == 0 {
			return ""
		}
		return strings.TrimSpace(anyToString(typed[0]))
	default:
		return ""
	}
}

func codeCallContextType(item map[string]any) string {
	if contextType := strings.TrimSpace(anyToString(item["context_type"])); contextType != "" {
		return contextType
	}

	contextTuple, ok := item["context"].([]any)
	if !ok || len(contextTuple) < 2 {
		return ""
	}
	return strings.TrimSpace(anyToString(contextTuple[1]))
}
