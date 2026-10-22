// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// toolIntent is the curated router metadata for one MCP tool: the short
// phrases an agent might type when it needs the tool, and whether the tool is
// the first one to try in its family of overlapping tools.
type toolIntent struct {
	phrases   []string
	canonical bool
}

// intents builds the router metadata for a tool that is not the canonical
// member of its family.
func intents(phrases ...string) toolIntent {
	return toolIntent{phrases: phrases}
}

// canonicalIntents builds the router metadata for the tool an agent should try
// first in its family. The family table in annotations_intents_families.go
// names the family and the reason; exactly one member may use this helper.
func canonicalIntents(phrases ...string) toolIntent {
	return toolIntent{phrases: phrases, canonical: true}
}

// toolIntentSources returns the per-family intent maps. They are kept apart so
// a test can detect a tool curated in two files, which a merged map would hide.
func toolIntentSources() []map[string]toolIntent {
	return []map[string]toolIntent{
		codeToolIntents(),
		repoServiceToolIntents(),
		supplyToolIntents(),
		iacDriftToolIntents(),
		opsToolIntents(),
	}
}

// toolIntents merges the per-family intent maps into one map keyed by MCP tool
// name.
func toolIntents() map[string]toolIntent {
	merged := make(map[string]toolIntent)
	for _, source := range toolIntentSources() {
		for name, ti := range source {
			merged[name] = ti
		}
	}
	return merged
}

// applyToolIntents copies the curated router metadata onto the overlay entries
// that already carry a backend and cost. A name with no overlay entry is
// skipped, not created: an intent cannot invent a surface, and
// TestIntentSourcesAreConsistent reports the stale key.
func applyToolIntents(merged map[string]Annotation) {
	for name, ti := range toolIntents() {
		a, ok := merged[name]
		if !ok {
			continue
		}
		a.Intents = append([]string(nil), ti.phrases...)
		a.Canonical = ti.canonical
		merged[name] = a
	}
}
