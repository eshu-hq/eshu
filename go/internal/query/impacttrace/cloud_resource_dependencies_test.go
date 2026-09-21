// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestLoadMaterializedServiceCloudResourceDependenciesOrderByIsTotalKey pins
// the #6782 entry-57 lesson: ORDER BY over a non-unique key leaves delivery
// order backend-undefined, and this statement paginates, so tied (name, id)
// rows flip between NornicDB and Neo4j runs. Every projected alias must be a
// sort key; otherwise a future column reopens nondeterministic pagination.
func TestLoadMaterializedServiceCloudResourceDependenciesOrderByIsTotalKey(t *testing.T) {
	t.Parallel()

	var captured string
	reader := querytestutil.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		captured = cypher
		return nil, nil
	}}

	_, err := LoadMaterializedServiceCloudResourceDependencies(t.Context(), reader, "repo:proof", "workload:proof", 10)
	if err != nil {
		t.Fatalf("LoadMaterializedServiceCloudResourceDependencies() error = %v", err)
	}

	aliases := returnAliases(t, captured)
	keys := orderKeys(t, captured)
	var missing []string
	for _, alias := range aliases {
		if !keys[alias] {
			missing = append(missing, alias)
		}
	}
	if len(missing) > 0 {
		t.Errorf("ORDER BY misses projected aliases %q (keys %s)", missing, sortedKeys(keys))
	}
}

// sortedKeys renders the ORDER BY key set compactly for failure messages.
func sortedKeys(keys map[string]bool) string {
	list := make([]string, 0, len(keys))
	for key := range keys {
		list = append(list, key)
	}
	sort.Strings(list)
	return "[" + strings.Join(list, " ") + "]"
}

// returnAliases extracts the projected column aliases between RETURN and
// ORDER BY, splitting only on top-level commas so coalesce arguments stay
// intact.
func returnAliases(t *testing.T, cypher string) []string {
	t.Helper()

	upper := strings.ToUpper(cypher)
	ret := strings.Index(upper, "RETURN")
	ord := strings.Index(upper, "ORDER BY")
	if ret < 0 || ord < 0 || ord < ret {
		t.Fatalf("statement lacks RETURN..ORDER BY shape: %q", cypher)
	}
	var aliases []string
	for _, projection := range splitTopLevel(cypher[ret+len("RETURN"):ord], ',') {
		alias := projection
		if i := strings.LastIndex(strings.ToUpper(projection), " AS "); i >= 0 {
			alias = projection[i+len(" AS "):]
		}
		alias = strings.TrimSpace(alias)
		if alias == "" {
			t.Fatalf("empty projection alias in %q", cypher)
		}
		aliases = append(aliases, alias)
	}
	return aliases
}

// orderKeys extracts the ORDER BY key set between ORDER BY and the closing
// LIMIT, stripping any ASC/DESC direction so keys compare by column.
func orderKeys(t *testing.T, cypher string) map[string]bool {
	t.Helper()

	upper := strings.ToUpper(cypher)
	ord := strings.Index(upper, "ORDER BY")
	if ord < 0 {
		t.Fatalf("statement lacks ORDER BY: %q", cypher)
	}
	rest := cypher[ord+len("ORDER BY"):]
	if i := strings.Index(strings.ToUpper(rest), "LIMIT"); i >= 0 {
		rest = rest[:i]
	}
	keys := map[string]bool{}
	for _, key := range splitTopLevel(rest, ',') {
		key = strings.TrimSpace(key)
		key = strings.TrimSuffix(key, " DESC")
		key = strings.TrimSuffix(key, " ASC")
		key = strings.TrimSpace(key)
		if key == "" {
			t.Fatalf("empty ORDER BY key in %q", cypher)
		}
		keys[key] = true
	}
	return keys
}

// splitTopLevel splits s on sep except inside parentheses.
func splitTopLevel(s string, sep rune) []string {
	var parts []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case sep:
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}
