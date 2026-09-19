// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// GraphElementProperties is one graph node or relationship and its full
// property map, as the gate reads it back from the backend.
type GraphElementProperties struct {
	// Kind is "node" or "edge".
	Kind string
	// Name is the relationship type for an edge, or the sorted labels joined
	// with ":" for a node.
	Name string
	// Properties is the element's property map as the Bolt driver returns it.
	Properties map[string]any
}

// unresolvedRowToken matches the literal text NornicDB v1.3.3 stores for a
// `row.<key>` read whose UNWIND row map omitted the key (#6782). `row` is the
// only UNWIND binding in the write path that ranges over maps.
var unresolvedRowToken = regexp.MustCompile(`^row\.([A-Za-z_][A-Za-z0-9_]*)$`)

// isUnresolvedRowToken reports whether value is an unresolved read of prop.
// The bare pattern also matches real data such as a file named "row.go", so a
// token counts only when its key is the property's own name (1249 of the 1437
// `x.<prop> = row.<key>` writes in the write path) or a snake_case multi-word
// key, which a file extension or a dotted identifier does not look like.
func isUnresolvedRowToken(prop, value string) bool {
	m := unresolvedRowToken.FindStringSubmatch(value)
	if m == nil {
		return false
	}
	return m[1] == prop || strings.Contains(m[1], "_")
}

// maxRowTokenExamples caps the offending groups a failing finding names.
const maxRowTokenExamples = 10

// EvaluateUnresolvedRowTokens fails when any node or edge property, or any
// string element of a list property, holds an unresolved `row.<key>` token.
// The check is corpus-size independent and always required: a correct writer
// never stores its own Cypher expression text, on either backend.
func EvaluateUnresolvedRowTokens(elements []GraphElementProperties) Finding {
	groups := map[string]int{}
	total := 0
	for _, el := range elements {
		for key, value := range el.Properties {
			for _, token := range rowTokens(key, value) {
				groups[fmt.Sprintf("%s %s.%s=%s", el.Kind, el.Name, key, token)]++
				total++
			}
		}
	}
	detail := fmt.Sprintf("no node or edge property holds an unresolved row.<key> token across %d elements", len(elements))
	if total > 0 {
		keys := make([]string, 0, len(groups))
		for k := range groups {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		examples := make([]string, 0, maxRowTokenExamples)
		for _, k := range keys {
			if len(examples) == maxRowTokenExamples {
				examples = append(examples, fmt.Sprintf("... %d more groups", len(keys)-maxRowTokenExamples))
				break
			}
			examples = append(examples, fmt.Sprintf("%s (%d)", k, groups[k]))
		}
		detail = fmt.Sprintf("%d element properties hold an unresolved row.<key> token (a writer omitted an UNWIND row key it reads): %s",
			total, strings.Join(examples, "; "))
	}
	return Finding{
		Phase:    "graph",
		Check:    "unresolved_row_tokens",
		OK:       total == 0,
		Required: true,
		Detail:   detail,
	}
}

// rowTokens returns every unresolved row token in property prop: the value
// itself when it is a string, or each matching string element of a list.
func rowTokens(prop string, value any) []string {
	switch v := value.(type) {
	case string:
		if isUnresolvedRowToken(prop, v) {
			return []string{v}
		}
	case []any:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok && isUnresolvedRowToken(prop, s) {
				out = append(out, s)
			}
		}
		return out
	case []string:
		var out []string
		for _, s := range v {
			if isUnresolvedRowToken(prop, s) {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
