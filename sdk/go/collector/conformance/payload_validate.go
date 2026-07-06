// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package conformance

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// validatePayload checks a decoded fact payload against the compiled schema,
// returning an error naming the first offending field. It first normalizes the
// payload through a JSON round trip so native Go values an out-of-tree caller's
// Collect() may emit (int, int64, []string, map[string]string, and other
// json.Marshal-able kinds) are compared as the canonical JSON types the schema
// describes — the same normalization the wire path and the SDK validator apply.
// Without it, a valid payload built from native Go types would be reported as an
// unknown type and falsely fail. A payload that cannot be marshaled to JSON is a
// classified failure, not a panic.
func (s payloadSchema) validatePayload(payload map[string]any) error {
	normalized, err := normalizePayload(payload)
	if err != nil {
		return err
	}
	return s.root.validate("", normalized)
}

// normalizePayload round-trips a payload through encoding/json so every value is
// one of the canonical decoded JSON kinds (map[string]any, []any, float64,
// bool, string, nil). This lets the validator accept native Go values without a
// separate reflection path, and matches how the payload is actually serialized
// on the wire.
func normalizePayload(payload map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("payload is not JSON serializable: %w", err)
	}
	var normalized map[string]any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, fmt.Errorf("payload could not be re-decoded as JSON: %w", err)
	}
	return normalized, nil
}

// validate checks a decoded object value against this object schema. path is the
// dotted field prefix for error messages (empty at the payload root). A required
// field that is absent or explicitly null, a declared property whose value has
// the wrong type, or an additional property that violates the value-type
// constraint is a violation. Undeclared keys are allowed unless a value-type
// constraint applies.
func (o objectSchema) validate(path string, value map[string]any) error {
	for _, field := range o.required {
		v, ok := value[field]
		if !ok || v == nil {
			return fmt.Errorf("missing required field %q", qualify(path, field))
		}
	}
	for name, child := range value {
		if prop, ok := o.properties[name]; ok {
			if err := prop.validateValue(qualify(path, name), child); err != nil {
				return err
			}
			continue
		}
		if o.valueType != "" {
			if err := checkPrimitiveType(qualify(path, name), o.valueType, child); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateValue checks one property value against a property schema, naming the
// field on mismatch and recursing into arrays and nested objects.
func (p propertySchema) validateValue(field string, value any) error {
	if value == nil {
		if _, ok := p.types["null"]; ok {
			return nil
		}
		return fmt.Errorf("field %q is null but the schema does not allow null", field)
	}

	actual := jsonTypeOf(value)
	if !typeSetAccepts(p.types, actual) {
		return fmt.Errorf("field %q has type %s, want one of %s", field, actual, sortedTypeList(p.types))
	}
	if p.format != "" {
		if err := validateStringFormat(field, p.format, value); err != nil {
			return err
		}
	}

	switch actual {
	case "array":
		if p.items == nil {
			return nil
		}
		elements, _ := value.([]any)
		for index, element := range elements {
			if err := p.items.validateValue(fmt.Sprintf("%s[%d]", field, index), element); err != nil {
				return err
			}
		}
	case "object":
		if p.object == nil {
			return nil
		}
		entries, _ := value.(map[string]any)
		if err := p.object.validate(field, entries); err != nil {
			return err
		}
	}
	return nil
}

// validateStringFormat checks a value against a supported JSON Schema string
// format. Compile-time checks ensure this is only called for string-typed
// values, so a failed type assertion here is a defensive validator error.
func validateStringFormat(field, format string, value any) error {
	typed, ok := value.(string)
	if !ok {
		return fmt.Errorf("field %q has type %s, want string format %s", field, jsonTypeOf(value), format)
	}
	switch format {
	case "date-time":
		if _, err := time.Parse(time.RFC3339Nano, typed); err != nil {
			return fmt.Errorf("field %q is not a valid date-time: %w", field, err)
		}
	default:
		return fmt.Errorf("field %q uses unsupported string format %q", field, format)
	}
	return nil
}

// checkPrimitiveType asserts a value matches a single primitive type name,
// naming the field on mismatch. It is used for additionalProperties value-type
// constraints (the string-map shape).
func checkPrimitiveType(field, want string, value any) error {
	if value == nil {
		return fmt.Errorf("field %q is null but the schema requires %s", field, want)
	}
	if actual := jsonTypeOf(value); !typeAccepts(want, actual) {
		return fmt.Errorf("field %q has type %s, want %s", field, actual, want)
	}
	return nil
}

// typeSetAccepts reports whether any schema type in the set accepts a value of
// the actual JSON type.
func typeSetAccepts(types map[string]struct{}, actual string) bool {
	for schemaType := range types {
		if typeAccepts(schemaType, actual) {
			return true
		}
	}
	return false
}

// typeAccepts reports whether a schema type accepts a value of the actual JSON
// type. An exact match always accepts; additionally an integer value satisfies a
// "number"-typed schema, since JSON Schema treats integers as a subset of
// numbers. No checked-in schema uses a bare "number" type today, but this keeps
// the validator correct if one does rather than falsely rejecting a whole-number
// value.
func typeAccepts(schemaType, actual string) bool {
	if schemaType == actual {
		return true
	}
	return schemaType == "number" && actual == "integer"
}

// jsonTypeOf reports the JSON Schema type name of a value. Payloads are
// normalized through a JSON round trip before validation (see normalizePayload),
// so in practice every value is a canonical decoded kind: map[string]any, []any,
// float64, bool, string, or nil. encoding/json decodes every JSON number to
// float64, so an integral float64 is reported as "integer" (satisfying an
// integer-typed schema) while a fractional float64 is "number". The json.Number
// and default arms remain as defensive fallbacks.
func jsonTypeOf(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if typed == float64(int64(typed)) {
			return "integer"
		}
		return "number"
	case json.Number:
		if strings.ContainsAny(string(typed), ".eE") {
			return "number"
		}
		return "integer"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}

// qualify joins a dotted field path prefix with a child field name.
func qualify(prefix, field string) string {
	if prefix == "" {
		return field
	}
	return prefix + "." + field
}

// sortedTypeList renders a type set as a sorted, comma-separated list for a
// deterministic error message.
func sortedTypeList(types map[string]struct{}) string {
	names := make([]string, 0, len(types))
	for name := range types {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
