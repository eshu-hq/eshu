// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import "sort"

// ExampleArguments builds an arguments object for schema that fills every
// required property with a schema-valid value: the first enum value, else
// the default, else a typed placeholder. Optional properties are left out.
// The mcp root tests that the example for every registered tool passes
// ValidateArguments.
func ExampleArguments(schema any) map[string]any {
	return placeholderArguments(schema, false)
}

// placeholderArguments fills the required properties of an object schema,
// or every property when all is true.
func placeholderArguments(raw any, all bool) map[string]any {
	out := map[string]any{}
	schema, ok := raw.(map[string]any)
	if !ok {
		return out
	}
	properties, _ := schema["properties"].(map[string]any)
	names := requiredNames(schema["required"])
	if all {
		names = names[:0]
		for name := range properties {
			names = append(names, name)
		}
		sort.Strings(names)
	}
	for _, name := range names {
		out[name] = placeholderValue(name, properties[name], all)
	}
	return out
}

func placeholderValue(name string, raw any, all bool) any {
	schema, _ := raw.(map[string]any)
	if options, ok := asSlice(schema["enum"]); ok && len(options) > 0 {
		return options[0]
	}
	if value, ok := schema["default"]; ok && value != nil {
		return value
	}
	switch schema["type"] {
	case "integer", "number":
		return numberPlaceholder(schema)
	case "boolean":
		return false
	case "array":
		return []any{}
	case "object":
		return placeholderArguments(schema, all)
	}
	if schema["format"] == "date-time" {
		return "2026-01-01T00:00:00Z"
	}
	return "<" + name + ">"
}

func numberPlaceholder(schema map[string]any) any {
	value := 1.0
	if minimum, ok := asNumber(schema["minimum"]); ok && value < minimum {
		value = minimum
	}
	if maximum, ok := asNumber(schema["maximum"]); ok && value > maximum {
		value = maximum
	}
	if schema["type"] == "integer" {
		return int(value)
	}
	return value
}
