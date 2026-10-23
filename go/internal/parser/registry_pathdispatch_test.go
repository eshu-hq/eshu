// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"strings"
	"testing"
)

func TestPathDispatchesResolveToNamedDefinition(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	dispatches := PathDispatches()
	if len(dispatches) == 0 {
		t.Fatal("PathDispatches() is empty")
	}
	for _, dispatch := range dispatches {
		if len(dispatch.Examples) == 0 {
			t.Errorf("%v has no example path", dispatch.Patterns)
		}
		for _, example := range dispatch.Examples {
			definition, ok := registry.LookupByPath(example)
			if !ok {
				t.Errorf("LookupByPath(%q) found nothing, want %s", example, dispatch.ParserKey)
				continue
			}
			if definition.ParserKey != dispatch.ParserKey {
				t.Errorf("LookupByPath(%q) = %s, want %s", example, definition.ParserKey, dispatch.ParserKey)
			}
		}
	}
}

func TestPathDispatchesCoverEveryHardcodedBinding(t *testing.T) {
	t.Parallel()

	// Each key below is bound by hardcoded path logic in LookupByPath that no
	// Extensions, ExactNames, or PrefixNames list captures. A new hardcoded
	// binding must be added to PathDispatches and to this list.
	want := map[string]bool{"java_metadata": false, "scala": false, "hcl": false}
	for _, dispatch := range PathDispatches() {
		if _, ok := want[dispatch.ParserKey]; ok {
			want[dispatch.ParserKey] = true
		}
	}
	for key, seen := range want {
		if !seen {
			t.Errorf("PathDispatches() has no entry for %s", key)
		}
	}
}

func TestAdmissionTableMarkdownListsPathDispatches(t *testing.T) {
	t.Parallel()

	table := AdmissionTableMarkdown(DefaultRegistry())
	for _, dispatch := range PathDispatches() {
		for _, pattern := range dispatch.Patterns {
			if !strings.Contains(table, "`"+pattern+"`") {
				t.Errorf("admission table misses path-dispatch pattern %q", pattern)
			}
		}
		if !strings.Contains(table, "`"+dispatch.ParserKey+"`") {
			t.Errorf("admission table misses path-dispatch parser key %q", dispatch.ParserKey)
		}
	}
	if !strings.Contains(table, "\n\nPath-dispatched bindings") {
		t.Error("admission table has no path-dispatch footnote after the table")
	}
}
