// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package php

import "strings"

func resolvePHPReferenceChainType(
	rootType string,
	segments []string,
	classPropertyTypes map[string]map[string]string,
	methodReturnTypes map[string]map[string]string,
	functionReturnTypes map[string]string,
	importAliases map[string]string,
) string {
	currentType := strings.TrimSpace(rootType)
	if currentType == "" {
		return ""
	}
	for _, segment := range segments {
		segmentName := strings.TrimSpace(segment)
		if segmentName == "" {
			return ""
		}
		if strings.HasSuffix(segmentName, ")") {
			openIndex := strings.Index(segmentName, "(")
			if openIndex < 0 {
				return ""
			}
			methodName := strings.TrimSpace(segmentName[:openIndex])
			if methodName == "" {
				return ""
			}
			nextType := lookupPHPMethodReturnType(currentType, methodName, methodReturnTypes, importAliases)
			if nextType == "" {
				return ""
			}
			currentType = nextType
			continue
		}
		nextType := normalizePHPImportedTypeName(classPropertyTypes[currentType][segmentName], importAliases)
		if nextType == "" {
			return ""
		}
		currentType = nextType
	}
	return currentType
}

func normalizePHPImportedTypeName(raw string, importAliases map[string]string) string {
	normalized := normalizePHPTypeName(raw)
	if normalized == "" || len(importAliases) == 0 {
		return normalized
	}
	resolved := strings.TrimSpace(importAliases[normalized])
	if resolved == "" || resolved == normalized {
		return normalized
	}
	return normalizePHPImportedTypeName(resolved, importAliases)
}

func normalizePHPParenthesizedReceiverExpression(raw string) string {
	trimmed := strings.TrimSpace(raw)
	for trimmed != "" && strings.HasPrefix(trimmed, "(") {
		closeIndex := findPHPMatchingParen(trimmed)
		if closeIndex < 0 {
			return trimmed
		}
		remainder := strings.TrimSpace(trimmed[closeIndex+1:])
		if remainder != "" && !strings.HasPrefix(remainder, "->") && !strings.HasPrefix(remainder, "::") {
			return trimmed
		}
		trimmed = strings.TrimSpace(trimmed[1:closeIndex]) + remainder
	}
	return trimmed
}

func findPHPMatchingParen(raw string) int {
	depth := 0
	for index, r := range raw {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index
			}
			if depth < 0 {
				return -1
			}
		}
	}
	return -1
}
