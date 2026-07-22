// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

func evaluateJSONPathRequirements(shape QueryShape, body []byte) (bool, string) {
	if len(shape.RequiredJSONPaths) == 0 && len(shape.RequiredJSONValues) == 0 &&
		len(shape.RequiredJSONObjectMatches) == 0 && len(shape.RequiredAbsentWhenPresent) == 0 {
		return true, ""
	}
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return false, "response is not valid JSON for path assertions: " + err.Error()
	}
	for _, path := range shape.RequiredJSONPaths {
		values, err := resolveJSONPath(root, path)
		if err != nil {
			return false, fmt.Sprintf("required JSON path %q failed: %v", path, err)
		}
		if !hasNonEmptyJSONValue(values) {
			return false, fmt.Sprintf("required JSON path %q resolved no non-empty values", path)
		}
	}
	for _, path := range sortedJSONValuePaths(shape.RequiredJSONValues) {
		expected := shape.RequiredJSONValues[path]
		values, err := resolveJSONPath(root, path)
		if err != nil {
			return false, fmt.Sprintf("required JSON value %q failed: %v", path, err)
		}
		if !hasMatchingJSONValue(values, expected) {
			return false, fmt.Sprintf("required JSON value %q did not equal %v", path, expected)
		}
	}
	for _, path := range sortedJSONObjectMatchPaths(shape.RequiredJSONObjectMatches) {
		values, err := resolveJSONPath(root, path)
		if err != nil {
			return false, fmt.Sprintf("required JSON object match %q failed: %v", path, err)
		}
		for _, expected := range shape.RequiredJSONObjectMatches[path] {
			if !hasMatchingJSONObject(values, expected) {
				return false, fmt.Sprintf("required JSON object match %q did not contain %v", path, expected)
			}
		}
	}
	for _, absent := range shape.RequiredAbsentWhenPresent {
		if ok, detail := evaluateAbsentWhenPresent(root, absent); !ok {
			return false, detail
		}
	}
	return true, fmt.Sprintf(
		"json paths %v, values %v, object matches %v, and mutual-exclusion checks %d present",
		shape.RequiredJSONPaths,
		sortedJSONValuePaths(shape.RequiredJSONValues),
		sortedJSONObjectMatchPaths(shape.RequiredJSONObjectMatches),
		len(shape.RequiredAbsentWhenPresent),
	)
}

// evaluateAbsentWhenPresent checks one AbsentWhenPresent mutual-exclusion
// assertion against the parsed response root. It fails only when SiblingPath
// resolves to a non-empty value AND DomainPath resolves DomainValue among its
// values in the SAME response; an unresolvable or empty SiblingPath (nothing
// served) or a DomainPath that never matches DomainValue (the domain is not
// disclosed as absent) both pass vacuously, matching the "existence assertions
// guard the rest" contract documented on AbsentWhenPresent.
func evaluateAbsentWhenPresent(root any, absent AbsentWhenPresent) (bool, string) {
	siblingValues, err := resolveJSONPath(root, absent.SiblingPath)
	if err != nil || !hasNonEmptyJSONValue(siblingValues) {
		return true, ""
	}
	domainValues, err := resolveJSONPath(root, absent.DomainPath)
	if err != nil {
		return true, ""
	}
	if hasMatchingJSONValue(domainValues, absent.DomainValue) {
		return false, fmt.Sprintf(
			"disclosure-vs-served contradiction: domain %q disclosed absent at %q while sibling %q is present",
			absent.DomainValue, absent.DomainPath, absent.SiblingPath,
		)
	}
	return true, ""
}

func resolveJSONPath(root any, path string) ([]any, error) {
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	current := []any{root}
	for _, rawSegment := range strings.Split(path, ".") {
		if rawSegment == "" {
			return nil, fmt.Errorf("empty segment in %q", path)
		}
		arraySegment := strings.HasSuffix(rawSegment, "[]")
		segment := strings.TrimSuffix(rawSegment, "[]")
		if segment == "" {
			return nil, fmt.Errorf("empty array segment in %q", path)
		}
		next := make([]any, 0, len(current))
		for _, value := range current {
			next = appendPathSegment(next, value, segment, arraySegment)
		}
		if len(next) == 0 {
			return nil, fmt.Errorf("path segment %q resolved no values", rawSegment)
		}
		current = next
	}
	return current, nil
}

func appendPathSegment(out []any, value any, segment string, arraySegment bool) []any {
	if arr, ok := value.([]any); ok && !arraySegment {
		for _, item := range arr {
			out = appendPathSegment(out, item, segment, false)
		}
		return out
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return out
	}
	field, ok := obj[segment]
	if !ok || field == nil {
		return out
	}
	if !arraySegment {
		return append(out, field)
	}
	arr, ok := field.([]any)
	if !ok || len(arr) == 0 {
		return out
	}
	return append(out, arr...)
}

func hasNonEmptyJSONValue(values []any) bool {
	for _, value := range values {
		switch v := value.(type) {
		case nil:
		case string:
			if v != "" {
				return true
			}
		case []any:
			if len(v) > 0 {
				return true
			}
		case map[string]any:
			if len(v) > 0 {
				return true
			}
		default:
			return true
		}
	}
	return false
}

func hasMatchingJSONValue(values []any, expected any) bool {
	for _, value := range values {
		if reflect.DeepEqual(value, expected) {
			return true
		}
	}
	return false
}

func hasMatchingJSONObject(values []any, expected map[string]any) bool {
	if len(expected) == 0 {
		return false
	}
	for _, value := range values {
		candidate, ok := value.(map[string]any)
		if !ok {
			continue
		}
		matches := true
		for key, expectedValue := range expected {
			if actual, exists := candidate[key]; !exists || !reflect.DeepEqual(actual, expectedValue) {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func sortedJSONValuePaths(values map[string]any) []string {
	paths := make([]string, 0, len(values))
	for path := range values {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func sortedJSONObjectMatchPaths(values map[string][]map[string]any) []string {
	paths := make([]string, 0, len(values))
	for path := range values {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
