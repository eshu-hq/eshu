// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"fmt"
	"sort"
)

// implementedKeywords are the JSON-schema keywords ValidateArguments either
// enforces or deliberately treats as annotations (description, default).
var implementedKeywords = map[string]bool{
	"type":                 true,
	"properties":           true,
	"required":             true,
	"additionalProperties": true,
	"items":                true,
	"enum":                 true,
	"minimum":              true,
	"maximum":              true,
	"minLength":            true,
	"maxItems":             true,
	"pattern":              true,
	"format":               true,
	"description":          true,
	"default":              true,
}

// implementedTypes are the scalar and container types matchesType checks.
var implementedTypes = map[string]bool{
	"string": true, "integer": true, "number": true, "boolean": true, "array": true, "object": true,
}

// implementedFormats are the format values validateString checks.
var implementedFormats = map[string]bool{"date-time": true}

// UnsupportedSchemaFeatures walks an input schema and returns, sorted, every
// keyword, type, or format ValidateArguments does not implement, plus any
// pattern Go's regexp engine cannot compile. Each finding reads
// "<path>: keyword <name>", "<path>: type <name>", "<path>: format <name>", or
// "<path>: pattern <expr>". An empty result means the validator can enforce the
// whole schema. The mcp root runs it over every registered tool, so a tool that
// adopts a new keyword fails a test instead of being half-validated.
func UnsupportedSchemaFeatures(schema any) []string {
	var findings []string
	walkSchema(schema, "", &findings)
	sort.Strings(findings)
	return findings
}

func walkSchema(raw any, path string, findings *[]string) {
	schema, ok := raw.(map[string]any)
	if !ok {
		return
	}
	at := path
	if at == "" {
		at = "$"
	}
	for keyword, value := range schema {
		if !implementedKeywords[keyword] {
			*findings = append(*findings, fmt.Sprintf("%s: keyword %s", at, keyword))
			continue
		}
		switch keyword {
		case "type":
			name, ok := value.(string)
			if !ok || !implementedTypes[name] {
				*findings = append(*findings, fmt.Sprintf("%s: type %v", at, value))
			}
		case "format":
			name, ok := value.(string)
			if !ok || !implementedFormats[name] {
				*findings = append(*findings, fmt.Sprintf("%s: format %v", at, value))
			}
		case "pattern":
			expr, ok := value.(string)
			if !ok {
				*findings = append(*findings, fmt.Sprintf("%s: pattern %v", at, value))
			} else if _, err := compilePattern(expr); err != nil {
				*findings = append(*findings, fmt.Sprintf("%s: pattern %s", at, expr))
			}
		case "properties":
			properties, ok := value.(map[string]any)
			if !ok {
				*findings = append(*findings, fmt.Sprintf("%s: keyword properties is %T", at, value))
				continue
			}
			for name, child := range properties {
				walkSchema(child, joinPath(path, "properties."+name), findings)
			}
		case "items", "additionalProperties":
			if _, ok := value.(map[string]any); !ok {
				*findings = append(*findings, fmt.Sprintf("%s: keyword %s is %T", at, keyword, value))
				continue
			}
			walkSchema(value, joinPath(path, keyword), findings)
		}
	}
}
