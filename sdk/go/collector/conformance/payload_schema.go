// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package conformance

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// payloadSchema is the decoded, minimal JSON Schema subset the conformance
// harness understands for a fact payload. It implements exactly the constructs
// the checked-in factschema payload schemas use
// (sdk/go/factschema/schema/*.json): an object with required keys and typed
// properties, where each property is a primitive/nullable type, an array with a
// typed item schema, or a nested object (open or a string-valued map). Every
// unrecognized keyword, type, or composition ($ref/oneOf/anyOf/allOf/enum/
// pattern/numeric bounds) is rejected at compile time (compileSchema) so the
// validator fails closed rather than silently under-validating a payload — the
// accuracy guarantee Contract System v1 exists to protect. Adding a schema
// construct means teaching this validator about it, never letting it slip
// through unchecked; the in-tree construct-coverage test turns the build red the
// moment a checked-in schema outgrows this subset.
type payloadSchema struct {
	root objectSchema
}

// objectSchema is the decoded constraint for a JSON object: its required keys
// and per-property schemas. It models both the top-level payload object and any
// nested object property, so nesting is handled by one recursive type rather
// than a special case per depth.
type objectSchema struct {
	// required lists the property names that must be present and non-null.
	required []string
	// properties maps a declared property name to its schema. A payload key not
	// present here is allowed (every payload schema is open,
	// additionalProperties: true, so collectors may emit context keys the
	// reducer ignores).
	properties map[string]propertySchema
	// valueType constrains the type of every additional (undeclared) property
	// value when set, modelling additionalProperties: {"type": ...} — the
	// aws_resource "tags" string-map shape. Empty means additionalProperties is
	// open (true) with no value-type constraint.
	valueType string
}

// propertySchema is the decoded constraint for one object property.
type propertySchema struct {
	// types is the set of JSON types the value may take, expanded from a bare
	// "type": "string" or a union "type": ["string", "null"]. Never empty after
	// compileProperty.
	types map[string]struct{}
	// items constrains array element values when "array" is an allowed type.
	// Nil when the property is not array-typed.
	items *propertySchema
	// object constrains the value when "object" is an allowed type. Nil when the
	// property is not object-typed.
	object *objectSchema
}

var (
	knownTopLevelKeywords = map[string]struct{}{
		"$id":                  {},
		"$schema":              {},
		"title":                {},
		"description":          {},
		"type":                 {},
		"required":             {},
		"properties":           {},
		"additionalProperties": {},
	}
	knownPropertyKeywords = map[string]struct{}{
		"type":                 {},
		"title":                {},
		"description":          {},
		"required":             {},
		"items":                {},
		"properties":           {},
		"additionalProperties": {},
	}
	supportedPrimitiveTypes = map[string]struct{}{
		"string":  {},
		"integer": {},
		"number":  {},
		"boolean": {},
		"array":   {},
		"object":  {},
		"null":    {},
	}
)

// CompileSchema reports whether a payload JSON Schema falls entirely within the
// subset this harness can validate, returning a descriptive error naming the
// first unsupported construct otherwise. A caller supplying schemas through
// Request.PayloadSchemas can call this once per schema to fail its own build the
// moment a checked-in schema outgrows the validator, rather than discovering a
// silently under-validated payload later. It performs no I/O.
func CompileSchema(raw json.RawMessage) error {
	_, err := compileSchema(raw)
	return err
}

// compileSchema decodes and validates one JSON Schema document into the
// internal payloadSchema, returning an error for any construct outside the
// supported subset. It fails closed on any unknown keyword, unknown type, or
// composition keyword.
func compileSchema(raw json.RawMessage) (payloadSchema, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return payloadSchema{}, fmt.Errorf("schema is not a JSON object: %w", err)
	}
	if err := rejectUnknownKeywords(doc, knownTopLevelKeywords); err != nil {
		return payloadSchema{}, err
	}
	if err := requireObjectType(doc); err != nil {
		return payloadSchema{}, err
	}
	root, err := compileObject(doc)
	if err != nil {
		return payloadSchema{}, err
	}
	return payloadSchema{root: root}, nil
}

// compileObject decodes the object-level constraints (required, properties,
// additionalProperties) shared by the top-level schema and any nested object
// property. The doc's keywords must already have been checked against the
// appropriate known-keyword set by the caller.
func compileObject(doc map[string]json.RawMessage) (objectSchema, error) {
	object := objectSchema{properties: map[string]propertySchema{}}

	if rawRequired, ok := doc["required"]; ok {
		if err := json.Unmarshal(rawRequired, &object.required); err != nil {
			return objectSchema{}, fmt.Errorf("\"required\" is not a string array: %w", err)
		}
	}

	if rawProps, ok := doc["properties"]; ok {
		var props map[string]json.RawMessage
		if err := json.Unmarshal(rawProps, &props); err != nil {
			return objectSchema{}, fmt.Errorf("\"properties\" is not an object: %w", err)
		}
		for name, rawProp := range props {
			prop, err := compileProperty(rawProp)
			if err != nil {
				return objectSchema{}, fmt.Errorf("property %q: %w", name, err)
			}
			object.properties[name] = prop
		}
	}

	valueType, err := compileAdditionalProperties(doc)
	if err != nil {
		return objectSchema{}, err
	}
	object.valueType = valueType

	return object, nil
}

// compileAdditionalProperties decodes an object's additionalProperties. It
// accepts a boolean (open or closed object, no value-type constraint returned)
// or a single-type schema {"type": <primitive>} constraining every additional
// value (the "tags" string-map shape). Any richer additionalProperties schema
// is rejected so it cannot pass unvalidated.
func compileAdditionalProperties(doc map[string]json.RawMessage) (string, error) {
	rawAP, ok := doc["additionalProperties"]
	if !ok {
		return "", nil
	}
	var asBool bool
	if err := json.Unmarshal(rawAP, &asBool); err == nil {
		return "", nil
	}
	var apObject map[string]json.RawMessage
	if err := json.Unmarshal(rawAP, &apObject); err != nil {
		return "", fmt.Errorf("\"additionalProperties\" must be a boolean or a typed schema")
	}
	if err := rejectUnknownKeywords(apObject, knownPropertyKeywords); err != nil {
		return "", fmt.Errorf("additionalProperties: %w", err)
	}
	rawAPType, ok := apObject["type"]
	if !ok {
		return "", fmt.Errorf("\"additionalProperties\" schema must declare a \"type\"")
	}
	apTypes, err := decodeTypeSet(rawAPType)
	if err != nil {
		return "", fmt.Errorf("additionalProperties: %w", err)
	}
	if len(apTypes) != 1 {
		return "", fmt.Errorf("\"additionalProperties\" schema must declare exactly one type")
	}
	for t := range apTypes {
		if t == "object" || t == "array" {
			return "", fmt.Errorf("\"additionalProperties\" value type %q is not supported", t)
		}
		return t, nil
	}
	return "", nil
}

// compileProperty decodes one property schema, failing closed on any
// unsupported keyword or type. Array and object types recurse into their
// element/object schemas.
func compileProperty(raw json.RawMessage) (propertySchema, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return propertySchema{}, fmt.Errorf("is not a JSON object: %w", err)
	}
	if err := rejectUnknownKeywords(doc, knownPropertyKeywords); err != nil {
		return propertySchema{}, err
	}

	prop := propertySchema{types: map[string]struct{}{}}

	rawType, ok := doc["type"]
	if !ok {
		return propertySchema{}, fmt.Errorf("missing \"type\"")
	}
	types, err := decodeTypeSet(rawType)
	if err != nil {
		return propertySchema{}, err
	}
	prop.types = types

	_, isArray := prop.types["array"]
	if isArray {
		rawItems, ok := doc["items"]
		if !ok {
			return propertySchema{}, fmt.Errorf("array property missing \"items\"")
		}
		items, err := compileProperty(rawItems)
		if err != nil {
			return propertySchema{}, fmt.Errorf("items: %w", err)
		}
		prop.items = &items
	} else if _, hasItems := doc["items"]; hasItems {
		return propertySchema{}, fmt.Errorf("\"items\" is only supported on an array-typed property")
	}

	_, isObject := prop.types["object"]
	if isObject {
		object, err := compileObject(doc)
		if err != nil {
			return propertySchema{}, err
		}
		prop.object = &object
	} else if _, hasProps := doc["properties"]; hasProps {
		return propertySchema{}, fmt.Errorf("\"properties\" is only supported on an object-typed property")
	} else if _, hasAP := doc["additionalProperties"]; hasAP {
		return propertySchema{}, fmt.Errorf("\"additionalProperties\" is only supported on an object-typed property")
	}

	return prop, nil
}

// decodeTypeSet expands a JSON Schema "type" value — a bare string or a union
// array of strings — into a set, rejecting any type outside the supported
// primitive set.
func decodeTypeSet(raw json.RawMessage) (map[string]struct{}, error) {
	types := map[string]struct{}{}

	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if _, ok := supportedPrimitiveTypes[single]; !ok {
			return nil, fmt.Errorf("unsupported type %q", single)
		}
		types[single] = struct{}{}
		return types, nil
	}

	var union []string
	if err := json.Unmarshal(raw, &union); err != nil {
		return nil, fmt.Errorf("\"type\" must be a string or an array of strings")
	}
	if len(union) == 0 {
		return nil, fmt.Errorf("\"type\" array must not be empty")
	}
	for _, t := range union {
		if _, ok := supportedPrimitiveTypes[t]; !ok {
			return nil, fmt.Errorf("unsupported type %q", t)
		}
		types[t] = struct{}{}
	}
	return types, nil
}

// rejectUnknownKeywords returns an error if doc carries any keyword not present
// in known, so an unsupported construct fails closed. The first unknown keyword
// (sorted) is named for a deterministic message.
func rejectUnknownKeywords(doc map[string]json.RawMessage, known map[string]struct{}) error {
	var unknown []string
	for keyword := range doc {
		if _, ok := known[keyword]; !ok {
			unknown = append(unknown, keyword)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("unsupported schema construct %q", unknown[0])
}

// requireObjectType asserts the top-level schema is typed as an object, the
// only top-level shape a fact payload takes.
func requireObjectType(doc map[string]json.RawMessage) error {
	rawType, ok := doc["type"]
	if !ok {
		return fmt.Errorf("schema must declare top-level \"type\": \"object\"")
	}
	var typeName string
	if err := json.Unmarshal(rawType, &typeName); err != nil {
		return fmt.Errorf("schema top-level \"type\" must be the string \"object\"")
	}
	if typeName != "object" {
		return fmt.Errorf("schema top-level \"type\" must be \"object\", got %q", typeName)
	}
	return nil
}

// validatePayload checks a decoded fact payload against the compiled schema,
// returning an error naming the first offending field.
func (s payloadSchema) validatePayload(payload map[string]any) error {
	return s.root.validate("", payload)
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

// jsonTypeOf reports the JSON Schema type name of a value decoded from JSON into
// Go's any (map[string]any / []any / float64 / bool / string / nil). encoding/json
// decodes every JSON number to float64, so an integral float64 is reported as
// "integer" (satisfying an integer-typed schema) while a fractional float64 is
// "number".
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
