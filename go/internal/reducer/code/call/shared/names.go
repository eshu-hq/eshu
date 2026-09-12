// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// ResolveSameFileCalleeEntityID resolves a call to a same-file entity by
// exact candidate names first, then (when the call has no qualified scope) by
// broader candidate names.
func ResolveSameFileCalleeEntityID(
	index EntityIndex,
	rawPath string,
	relativePath string,
	call map[string]any,
) string {
	language := CallLanguage(call, rawPath, relativePath)
	for _, name := range ExactCandidateNames(call, language) {
		for _, pathKey := range PathKeys(rawPath, relativePath) {
			if entityID := index.UniqueNameByPath(pathKey, name); entityID != "" {
				return entityID
			}
		}
	}
	if HasQualifiedScope(call, language) {
		return ""
	}
	for _, name := range BroadCandidateNames(call, language) {
		for _, pathKey := range PathKeys(rawPath, relativePath) {
			if entityID := index.UniqueNameByPath(pathKey, name); entityID != "" {
				return entityID
			}
		}
	}
	return ""
}

// ExactCandidateNames returns the qualified/exact candidate names for a call,
// honoring per-language full_name conventions.
func ExactCandidateNames(call map[string]any, language string) []string {
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

	name := payloadcore.AnyToString(call["name"])
	fullName := payloadcore.AnyToString(call["full_name"])
	if codeCallHasQualifiedFullName(fullName) {
		appendName(fullName)
		if codeCallJavaClassReferenceKind(call) {
			appendName(name)
		}
		if language == "python" && PythonQualifiedClassReceiver(fullName) {
			appendName(TrailingName(fullName))
		}
		if JavaScriptFamily(language) && strings.HasPrefix(fullName, "module.exports.") {
			appendName(TrailingName(fullName))
		}
		if JavaScriptFamily(language) {
			for _, receiver := range JavaScriptFunctionReceiverNames(fullName) {
				appendName(receiver)
				if strings.HasPrefix(receiver, "module.exports.") {
					appendName(TrailingName(receiver))
				}
			}
		}
		if language == "dart" {
			// Fail open (#5332 regression): the Dart AST rewrite emits a
			// receiver-qualified full_name ("repository.create") for BOTH
			// class/static/named-constructor references and ordinary
			// instance-variable receivers — unlike Python, which only adds a
			// bare fallback for class-style receivers
			// (PythonQualifiedClassReceiver). A variable receiver's
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
			// UniqueNameByRepo when exactly one declaration claims it.
			appendName(TrailingName(fullName))
		}
	}
	for _, classContext := range codeCallClassContexts(call) {
		appendName(classContext + "." + name)
	}
	inferredType := strings.TrimSpace(payloadcore.AnyToString(call["inferred_obj_type"]))
	if inferredType != "" && strings.TrimSpace(name) != "" {
		appendName(inferredType + "." + name)
		if language == "php" && strings.Contains(inferredType, "\\") {
			appendName(TrailingName(inferredType) + "." + name)
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
	if arity, ok := MetadataInt(call, "argument_count"); ok {
		names = AppendArityNames(names, arity)
	}
	if argumentTypes := MetadataStringSlice(call, "argument_types"); len(argumentTypes) > 0 {
		names = AppendTypedSignatureNames(names, argumentTypes)
	}
	return names
}

func codeCallJavaClassReferenceKind(call map[string]any) bool {
	switch strings.TrimSpace(payloadcore.AnyToString(call["call_kind"])) {
	case "java.reflection_class_reference", "java.service_loader_provider", "java.spring_autoconfiguration_class":
		return true
	default:
		return false
	}
}

// PythonQualifiedClassReceiver reports whether a qualified full_name's
// receiver segment is UpperCamelCase, marking it a class reference rather
// than an instance-variable receiver.
func PythonQualifiedClassReceiver(fullName string) bool {
	trimmed := strings.TrimSpace(fullName)
	dot := strings.LastIndex(trimmed, ".")
	if dot <= 0 || dot >= len(trimmed)-1 {
		return false
	}
	receiver := TrailingName(trimmed[:dot])
	if receiver == "" {
		return false
	}
	first := rune(receiver[0])
	return first >= 'A' && first <= 'Z'
}

// codeCallDartQualifiedClassReceiver mirrors PythonQualifiedClassReceiver's
// structure: it reports whether a Dart qualified full_name's receiver
// segment (the text before the final delimiter, with a leading "_" or "$"
// stripped) is UpperCamelCase and therefore names a class, static member, or
// named constructor rather than an instance-variable, keyword
// ("super"/"this"), multi-segment, or otherwise unrecognized receiver. Unlike
// the Python helper, this classification does not gate whether
// ExactCandidateNames appends the bare fallback for Dart — that append is
// unconditional (fail open, see the "dart" branch above) because a variable
// receiver has no other path to resolution. The classifier exists to make
// that distinction explainable and independently testable.
func codeCallDartQualifiedClassReceiver(fullName string) bool {
	trimmed := strings.TrimSpace(fullName)
	dot := strings.LastIndex(trimmed, ".")
	if dot <= 0 || dot >= len(trimmed)-1 {
		return false
	}
	receiver := strings.TrimLeft(TrailingName(trimmed[:dot]), "_$")
	if receiver == "" {
		return false
	}
	first := rune(receiver[0])
	return first >= 'A' && first <= 'Z'
}

// JavaScriptFunctionReceiverNames returns the receiver expression when
// fullName is a .call/.apply/.bind invocation, or nil otherwise.
func JavaScriptFunctionReceiverNames(fullName string) []string {
	fullName = strings.TrimSpace(fullName)
	for _, method := range []string{".call", ".apply", ".bind"} {
		if receiver, ok := strings.CutSuffix(fullName, method); ok && strings.TrimSpace(receiver) != "" {
			return []string{receiver}
		}
	}
	return nil
}

// JavaScriptFamily reports whether language is JavaScript, JSX, TypeScript,
// or TSX.
func JavaScriptFamily(language string) bool {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "javascript", "jsx", "typescript", "tsx":
		return true
	default:
		return false
	}
}

// BroadCandidateNames returns the broad (non-qualified) candidate names for a
// call, or nil for Elixir which has no useful broad fallback.
func BroadCandidateNames(call map[string]any, language string) []string {
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

	name := payloadcore.AnyToString(call["name"])
	fullName := payloadcore.AnyToString(call["full_name"])
	appendName(name)
	appendName(fullName)
	appendName(TrailingName(fullName))
	appendName(codeCallTrailingSegments(fullName, 2))
	if arity, ok := MetadataInt(call, "argument_count"); ok {
		names = AppendArityNames(names, arity)
	}
	if argumentTypes := MetadataStringSlice(call, "argument_types"); len(argumentTypes) > 0 {
		names = AppendTypedSignatureNames(names, argumentTypes)
	}
	return names
}

// TrailingName returns the final "."/":"/"#"/"/"/"\\"-delimited segment of
// value, or "" when value is blank.
func TrailingName(value string) string {
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

// PreferredPath returns the normalized relativePath when present, else the
// normalized rawPath.
func PreferredPath(rawPath string, relativePath string) string {
	if normalized := NormalizePath(relativePath); normalized != "" {
		return normalized
	}
	return NormalizePath(rawPath)
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

	name := payloadcore.AnyToString(item["name"])
	appendName(name)
	fullName := payloadcore.AnyToString(item["full_name"])
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
	if arity, ok := MetadataInt(item, "parameter_count"); ok {
		names = AppendArityNames(names, arity)
	}
	if parameterTypes := MetadataStringSlice(item, "parameter_types"); len(parameterTypes) > 0 {
		names = AppendTypedSignatureNames(names, parameterTypes)
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

	appendName(payloadcore.AnyToString(item["name"]))
	appendName(payloadcore.AnyToString(item["full_name"]))
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
		return strings.TrimSpace(payloadcore.AnyToString(typed[0]))
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
			appendContext(payloadcore.AnyToString(value))
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
		return strings.TrimSpace(payloadcore.AnyToString(typed[0]))
	default:
		return ""
	}
}

func codeCallContextType(item map[string]any) string {
	if contextType := strings.TrimSpace(payloadcore.AnyToString(item["context_type"])); contextType != "" {
		return contextType
	}

	contextTuple, ok := item["context"].([]any)
	if !ok || len(contextTuple) < 2 {
		return ""
	}
	return strings.TrimSpace(payloadcore.AnyToString(contextTuple[1]))
}
