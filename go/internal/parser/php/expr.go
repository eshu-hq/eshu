// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import "strings"

// phpParseNewExpressionType returns the instantiated type name from a
// `new Type(...)` expression string, or the empty string when the expression is
// not a direct instantiation. It does not descend into chained or static
// receivers, mirroring the receiver-inference contract.
func phpParseNewExpressionType(expr string) string {
	trimmed := strings.TrimSpace(expr)
	const keyword = "new "
	if !strings.HasPrefix(trimmed, keyword) {
		return ""
	}
	rest := strings.TrimSpace(trimmed[len(keyword):])
	openParen := strings.Index(rest, "(")
	if openParen < 0 {
		return ""
	}
	name := strings.TrimSpace(rest[:openParen])
	name = strings.TrimPrefix(name, `\`)
	if name == "" || !phpIsQualifiedTypeReference(name) {
		return ""
	}
	return name
}

// phpVariableRootName returns the bare identifier of a leading `$name` token in
// an expression, or the empty string when the token is not a plain variable.
func phpVariableRootName(expr string) string {
	trimmed := strings.TrimSpace(expr)
	if !strings.HasPrefix(trimmed, "$") {
		return ""
	}
	end := 1
	for end < len(trimmed) && phpIsIdentifierByte(trimmed[end], end == 1) {
		end++
	}
	if end == 1 {
		return ""
	}
	if end != len(trimmed) {
		return ""
	}
	return trimmed[1:end]
}

// phpParseStaticProperty splits a `Class::$prop` static property access string
// into its owner type and property name. The owner may be a namespaced type.
func phpParseStaticProperty(expr string) (string, string, bool) {
	trimmed := strings.TrimSpace(expr)
	separator := strings.Index(trimmed, "::$")
	if separator < 0 {
		return "", "", false
	}
	owner := strings.TrimSpace(trimmed[:separator])
	property := strings.TrimSpace(trimmed[separator+len("::$"):])
	if owner == "" || property == "" {
		return "", "", false
	}
	if !phpIsQualifiedTypeReference(owner) || !phpIsIdentifier(property) {
		return "", "", false
	}
	return owner, property, true
}

// phpIsQualifiedTypeReference reports whether name is a PHP type identifier that
// may include namespace separators.
func phpIsQualifiedTypeReference(name string) bool {
	for _, segment := range strings.Split(name, `\`) {
		if segment == "" {
			continue
		}
		if !phpIsIdentifier(segment) {
			return false
		}
	}
	return name != ""
}

func phpIsIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for index := 0; index < len(name); index++ {
		if !phpIsIdentifierByte(name[index], index == 0) {
			return false
		}
	}
	return true
}

func phpIsIdentifierByte(b byte, first bool) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b == '_':
		return true
	case b >= '0' && b <= '9':
		return !first
	default:
		return false
	}
}
