// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	parameterExpression = regexp.MustCompile(`^\$(\w+)((?:\.\w+)*)$`)
	aliasExpression     = regexp.MustCompile(`^(\w+)((?:\.\w+)*)$`)
)

// parametersProveNoID reports whether the bound parameters prove that the
// map expression of a dynamic `SET n += <expression>` has no id key. Only
// `$param`, `$param.path`, and `alias.path` (alias from `UNWIND $param AS
// alias`) resolve; anything else, a missing parameter object, or a map that
// carries an id key at any depth fails the proof.
func parametersProveNoID(expression string, unwindAliases map[string]string, parameters string) bool {
	if strings.TrimSpace(parameters) == "" {
		return false
	}
	var bound map[string]any
	if err := json.Unmarshal([]byte(parameters), &bound); err != nil {
		return false
	}
	expression = strings.TrimSpace(expression)
	var roots []any
	var path []string
	if m := parameterExpression.FindStringSubmatch(expression); m != nil {
		value, ok := bound[m[1]]
		if !ok {
			return false
		}
		roots = []any{value}
		path = splitPath(m[2])
	} else if m := aliasExpression.FindStringSubmatch(expression); m != nil {
		name, ok := unwindAliases[m[1]]
		if !ok {
			return false
		}
		value, ok := bound[name]
		if !ok {
			return false
		}
		list, isList := value.([]any)
		if !isList {
			list = []any{value}
		}
		roots = list
		path = splitPath(m[2])
	} else {
		return false
	}
	for _, root := range roots {
		value, found := descend(root, path)
		if found && containsIDKey(value) {
			return false
		}
	}
	return true
}

func splitPath(raw string) []string {
	raw = strings.Trim(raw, ".")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ".")
}

// descend follows path through nested maps. A missing step reports not found,
// which writes nothing.
func descend(value any, path []string) (any, bool) {
	for _, step := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[step]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

// containsIDKey reports whether any map inside value has an "id" key.
func containsIDKey(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		if _, ok := typed["id"]; ok {
			return true
		}
		for _, item := range typed {
			if containsIDKey(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsIDKey(item) {
				return true
			}
		}
	}
	return false
}

// parameterKeyIsNotID reports whether the key of a dynamic `SET n[<key>] = v`
// is a bound parameter whose string value is not "id". Any other key
// expression, including a literal (the scan blanks literals), is not proven.
func parameterKeyIsNotID(keyExpression, parameters string) bool {
	m := parameterExpression.FindStringSubmatch(strings.TrimSpace(keyExpression))
	if m == nil || m[2] != "" || strings.TrimSpace(parameters) == "" {
		return false
	}
	var bound map[string]any
	if err := json.Unmarshal([]byte(parameters), &bound); err != nil {
		return false
	}
	key, ok := bound[m[1]].(string)
	return ok && key != "id"
}
