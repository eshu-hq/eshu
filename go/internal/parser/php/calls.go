package php

import (
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func appendUniquePHPCall(
	payload map[string]any,
	seen map[string]struct{},
	name string,
	fullName string,
	lineNumber int,
	args []string,
	contextName string,
	contextKind string,
	contextLine int,
	inferredObjType string,
) {
	if strings.TrimSpace(fullName) == "" {
		return
	}
	seenKey := fullName + "#" + strconv.Itoa(lineNumber)
	if _, ok := seen[seenKey]; ok {
		return
	}
	seen[seenKey] = struct{}{}
	item := map[string]any{
		"name":          name,
		"full_name":     fullName,
		"line_number":   lineNumber,
		"args":          args,
		"context":       []any{contextName, contextKind, contextLine},
		"lang":          "php",
		"is_dependency": false,
	}
	if inferredObjType != "" {
		item["inferred_obj_type"] = inferredObjType
	} else {
		item["inferred_obj_type"] = nil
	}
	if contextKind == "class_declaration" || contextKind == "interface_declaration" || contextKind == "trait_declaration" {
		item["class_context"] = []any{contextName, contextLine}
	} else {
		item["class_context"] = []any{nil, nil}
	}
	shared.AppendBucket(payload, "function_calls", item)
}

func normalizePHPMethodCall(raw string) string {
	parts := strings.Split(normalizePHPParenthesizedReceiverExpression(raw), "->")
	if len(parts) <= 1 {
		return raw
	}
	return strings.Join(parts[:len(parts)-1], "->") + "." + parts[len(parts)-1]
}

func normalizePHPStaticReceiver(raw string, classContext string, classParentTypes map[string]string, importAliases map[string]string) string {
	receiver := strings.TrimSpace(raw)
	if receiver == "" {
		return ""
	}

	switch receiver {
	case "self", "static":
		if classContext != "" {
			return classContext
		}
	case "parent":
		if parentType := strings.TrimSpace(classParentTypes[classContext]); parentType != "" {
			return parentType
		}
		return ""
	}

	trimmed := strings.TrimPrefix(receiver, `\`)
	if importAliases != nil {
		if resolved := strings.TrimSpace(importAliases[trimmed]); resolved != "" {
			return normalizePHPImportedTypeName(resolved, importAliases)
		}
	}

	return trimmed
}

func hasPHPReceiverChainPrefix(raw string, start int) bool {
	if start < 2 || start > len(raw) {
		return false
	}
	prefix := raw[start-2 : start]
	return prefix == "->" || prefix == "::"
}
