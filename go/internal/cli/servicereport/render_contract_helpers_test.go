// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicereport

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

// Helpers shared by the render-contract tests: the reflect walk over
// serviceintel.Report, the JSON flattener, and the sentinel builder.
//
// Split out of render_contract_test.go when adding the next-call precedence
// test pushed that file past the 500-line cap. Review had already flagged it
// as approaching the limit and named these as the functions to move.

// walkReportType collects the JSON paths of t, descending through pointers,
// slices, and structs. It stops at any path renderKeyContract marks opaque,
// which is what keeps the truth envelopes and evidence-handle shapes from
// dragging another package's field list into this contract.
func walkReportType(t reflect.Type, path string) map[string]bool {
	out := map[string]bool{}
	walkReportTypeInto(t, path, out)
	return out
}

func walkReportTypeInto(t reflect.Type, path string, out map[string]bool) {
	if disposition, ok := renderKeyContract[path]; ok && disposition.opaque {
		out[path] = true
		return
	}
	switch t.Kind() {
	case reflect.Pointer:
		walkReportTypeInto(t.Elem(), path, out)
	case reflect.Slice, reflect.Array:
		walkReportTypeInto(t.Elem(), path+"[]", out)
	case reflect.Struct:
		for i := range t.NumField() {
			field := t.Field(i)
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if name == "" || name == "-" {
				continue
			}
			walkReportTypeInto(field.Type, path+"."+name, out)
		}
	default:
		out[path] = true
	}
}

// flattenReportJSON marshals report and returns every JSON path it emits,
// mapped to the string values found at that path. Array indices collapse to
// "[]". An opaque path collects every string anywhere beneath it, so one
// sentinel below an omitted subtree still proves the subtree stayed out of
// the text. Non-string scalars contribute no value: "true" or "0" would match
// unrelated text and turn an absence check into a coin flip.
func flattenReportJSON(t *testing.T, report serviceintel.Report) map[string][]string {
	t.Helper()
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode marshalled report: %v", err)
	}
	out := map[string][]string{}
	flattenJSONInto(decoded, "", out)
	return out
}

func flattenJSONInto(value any, path string, out map[string][]string) {
	if disposition, ok := renderKeyContract[path]; ok && disposition.opaque {
		out[path] = append(out[path], collectStrings(value)...)
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			flattenJSONInto(child, path+"."+key, out)
		}
	case []any:
		for _, child := range typed {
			flattenJSONInto(child, path+"[]", out)
		}
	case string:
		if typed != "" {
			out[path] = append(out[path], typed)
			return
		}
		if _, seen := out[path]; !seen {
			out[path] = nil
		}
	default:
		if _, seen := out[path]; !seen {
			out[path] = nil
		}
	}
}

// collectStrings returns every non-empty string leaf anywhere inside value.
// The empty string is skipped everywhere: strings.Contains always finds it, so
// an absence check against "" can never fail.
func collectStrings(value any) []string {
	switch typed := value.(type) {
	case map[string]any:
		var found []string
		for _, child := range typed {
			found = append(found, collectStrings(child)...)
		}
		sort.Strings(found)
		return found
	case []any:
		var found []string
		for _, child := range typed {
			found = append(found, collectStrings(child)...)
		}
		return found
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	default:
		return nil
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sentinel returns a value unlikely to occur in RenderReport's own wording, so
// finding it in the output can only mean the field was printed.
func sentinel(name string) string { return "SENTINEL-" + name + "-ZZ" }
