// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// FieldError is one argument that failed its tool's input schema.
type FieldError struct {
	// Path locates the failing value, for example "limit" or
	// "handles[0].kind". It is empty for the arguments object itself.
	Path string `json:"path"`
	// Keyword is the JSON-schema keyword that failed, such as "required",
	// "type", "minimum", or "enum".
	Keyword string `json:"keyword"`
	// Message says what was expected, in plain words.
	Message string `json:"message"`
}

// Output bounds for validation errors. Arguments are caller-controlled and the
// transport has no body cap, so the error list and every caller-supplied text
// it echoes are capped.
const (
	// MaxFieldErrors is the most field errors ValidateArguments returns. It
	// stops walking the arguments once it has that many.
	MaxFieldErrors = 20
	// MaxEchoBytes bounds caller-controlled text echoed in an error, such as
	// an object key inside a field-error path; longer text is cut and ends
	// with "...".
	MaxEchoBytes = 256
)

// errorSink collects field errors up to MaxFieldErrors.
type errorSink struct{ errs []FieldError }

func (c *errorSink) add(e FieldError) {
	if c.full() {
		return
	}
	e.Path = boundEcho(e.Path)
	c.errs = append(c.errs, e)
}

func (c *errorSink) full() bool { return len(c.errs) >= MaxFieldErrors }

// boundEcho cuts caller-controlled text to MaxEchoBytes on a rune boundary.
func boundEcho(text string) string {
	if len(text) <= MaxEchoBytes {
		return text
	}
	cut := MaxEchoBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "..."
}

// ValidateArguments checks args against a registered tool's input schema and
// returns its violations, ordered by path then keyword: all of them, or the
// first MaxFieldErrors in traversal order (object keys sorted, array items in
// order). A nil or empty result means the arguments are valid.
//
// It implements the keywords registered tools use (see
// UnsupportedSchemaFeatures) and mirrors the dispatch path's tolerance where
// strict JSON schema would reject a call dispatch accepts today: a JSON null
// for an optional property counts as absent, properties the schema does not
// declare are ignored, and a blank date-time is accepted because the handlers
// trim it to "not set". default and description are annotations and are never
// applied, so a validated call reaches dispatch byte-for-byte unchanged.
func ValidateArguments(schema any, args map[string]any) []FieldError {
	sink := &errorSink{}
	if args == nil {
		args = map[string]any{}
	}
	validateValue(schema, args, "", sink)
	errs := sink.errs
	sort.SliceStable(errs, func(i, j int) bool {
		if errs[i].Path != errs[j].Path {
			return errs[i].Path < errs[j].Path
		}
		return errs[i].Keyword < errs[j].Keyword
	})
	return errs
}

func validateValue(rawSchema any, value any, path string, errs *errorSink) {
	schema, ok := rawSchema.(map[string]any)
	if !ok || errs.full() {
		return
	}
	if typeName, ok := schema["type"].(string); ok && !matchesType(typeName, value) {
		errs.add(FieldError{Path: path, Keyword: "type", Message: fmt.Sprintf("must be %s, got %s", article(typeName), describe(value))})
		return
	}
	if enum, ok := schema["enum"]; ok && !enumContains(enum, value) {
		errs.add(FieldError{Path: path, Keyword: "enum", Message: fmt.Sprintf("must be one of %s", enumText(enum))})
	}
	if number, ok := asNumber(value); ok {
		validateNumber(schema, number, path, errs)
	}
	if text, ok := value.(string); ok {
		validateString(schema, text, path, errs)
	}
	if items, ok := asSlice(value); ok {
		validateArray(schema, items, path, errs)
	}
	if object, ok := asObject(value); ok {
		validateObject(schema, object, path, errs)
	}
}

func validateNumber(schema map[string]any, number float64, path string, errs *errorSink) {
	if limit, ok := asNumber(schema["minimum"]); ok && number < limit {
		errs.add(FieldError{Path: path, Keyword: "minimum", Message: fmt.Sprintf("must be at least %s", formatNumber(limit))})
	}
	if limit, ok := asNumber(schema["maximum"]); ok && number > limit {
		errs.add(FieldError{Path: path, Keyword: "maximum", Message: fmt.Sprintf("must be at most %s", formatNumber(limit))})
	}
}

func validateString(schema map[string]any, text string, path string, errs *errorSink) {
	if limit, ok := asNumber(schema["minLength"]); ok && float64(utf8.RuneCountInString(text)) < limit {
		errs.add(FieldError{Path: path, Keyword: "minLength", Message: fmt.Sprintf("must have at least %s characters", formatNumber(limit))})
	}
	if pattern, ok := schema["pattern"].(string); ok {
		if re, err := compilePattern(pattern); err == nil && !re.MatchString(text) {
			errs.add(FieldError{Path: path, Keyword: "pattern", Message: fmt.Sprintf("must match the pattern %q", pattern)})
		}
	}
	if format, ok := schema["format"].(string); ok && format == "date-time" {
		trimmed := strings.TrimSpace(text)
		if trimmed != "" {
			if _, err := time.Parse(time.RFC3339, trimmed); err != nil {
				errs.add(FieldError{Path: path, Keyword: "format", Message: "must be an RFC 3339 date-time such as 2026-01-02T15:04:05Z"})
			}
		}
	}
}

func validateArray(schema map[string]any, items []any, path string, errs *errorSink) {
	if limit, ok := asNumber(schema["maxItems"]); ok && float64(len(items)) > limit {
		// Stop here: per-item errors on an oversized array only inflate the
		// response.
		errs.add(FieldError{Path: path, Keyword: "maxItems", Message: fmt.Sprintf("must have at most %s items", formatNumber(limit))})
		return
	}
	itemSchema, ok := schema["items"]
	if !ok {
		return
	}
	for i, item := range items {
		if errs.full() {
			return
		}
		validateValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, i), errs)
	}
}

func validateObject(schema map[string]any, object map[string]any, path string, errs *errorSink) {
	for _, name := range requiredNames(schema["required"]) {
		if object[name] == nil {
			errs.add(FieldError{Path: joinPath(path, name), Keyword: "required", Message: "is required"})
		}
	}
	properties, _ := schema["properties"].(map[string]any)
	extra := schema["additionalProperties"]
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if errs.full() {
			return
		}
		value := object[key]
		if value == nil {
			// A JSON null reads as "not set" on the dispatch path; required
			// properties were reported above.
			continue
		}
		if propertySchema, ok := properties[key]; ok {
			validateValue(propertySchema, value, joinPath(path, key), errs)
			continue
		}
		if extraSchema, ok := extra.(map[string]any); ok {
			validateValue(extraSchema, value, joinPath(path, key), errs)
		}
	}
}

func matchesType(typeName string, value any) bool {
	switch typeName {
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		number, ok := asNumber(value)
		return ok && number == math.Trunc(number) && !math.IsInf(number, 0)
	case "number":
		_, ok := asNumber(value)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "array":
		_, ok := asSlice(value)
		return ok
	case "object":
		_, ok := asObject(value)
		return ok
	default:
		// UnsupportedSchemaFeatures reports unknown types; validation does not
		// reject a call for a schema it cannot interpret.
		return true
	}
}

// asNumber reads JSON numbers decoded as float64, json.Number, and the Go
// integer and float kinds in-process callers and tests send.
func asNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case nil, bool, string:
		return 0, false
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		return f, !math.IsNaN(f)
	default:
		return 0, false
	}
}

func asSlice(value any) ([]any, bool) {
	if items, ok := value.([]any); ok {
		return items, true
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	items := make([]any, rv.Len())
	for i := range items {
		items[i] = rv.Index(i).Interface()
	}
	return items, true
}

func asObject(value any) (map[string]any, bool) {
	if object, ok := value.(map[string]any); ok {
		return object, true
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil, false
	}
	object := make(map[string]any, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		object[iter.Key().String()] = iter.Value().Interface()
	}
	return object, true
}

func enumContains(enum any, value any) bool {
	options, ok := asSlice(enum)
	if !ok {
		return true
	}
	number, isNumber := asNumber(value)
	for _, option := range options {
		if option == value {
			return true
		}
		if optionNumber, ok := asNumber(option); ok && isNumber && optionNumber == number {
			return true
		}
	}
	return false
}

func enumText(enum any) string {
	options, _ := asSlice(enum)
	parts := make([]string, 0, len(options))
	for _, option := range options {
		parts = append(parts, fmt.Sprintf("%v", option))
	}
	return strings.Join(parts, ", ")
}

func requiredNames(raw any) []string {
	items, ok := asSlice(raw)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		if name, ok := item.(string); ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

var patternCache sync.Map

// compilePattern compiles and caches a schema pattern. Go's RE2 syntax is a
// subset of ECMA-262; UnsupportedSchemaFeatures reports a registered pattern
// RE2 cannot compile, so a valid registry never reaches the error branch.
func compilePattern(pattern string) (*regexp.Regexp, error) {
	if cached, ok := patternCache.Load(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	patternCache.Store(pattern, re)
	return re, nil
}

func joinPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "." + name
}

func describe(value any) string {
	if value == nil {
		return "null"
	}
	if _, ok := value.(string); ok {
		return "a string"
	}
	if _, ok := value.(bool); ok {
		return "a boolean"
	}
	if number, ok := asNumber(value); ok {
		if number == math.Trunc(number) {
			return "an integer"
		}
		return "a number"
	}
	if _, ok := asSlice(value); ok {
		return "an array"
	}
	if _, ok := asObject(value); ok {
		return "an object"
	}
	return fmt.Sprintf("%T", value)
}

func article(typeName string) string {
	switch typeName {
	case "integer", "object", "array":
		return "an " + typeName
	default:
		return "a " + typeName
	}
}

func formatNumber(number float64) string {
	if number == math.Trunc(number) {
		return fmt.Sprintf("%d", int64(number))
	}
	return fmt.Sprintf("%g", number)
}
