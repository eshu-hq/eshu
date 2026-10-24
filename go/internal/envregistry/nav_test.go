// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package envregistry

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	mkdocsRelPath    = "docs/mkdocs.yml"
	navSectionTitle  = "Environment Variable Registry (generated)"
	navReferencePath = "reference/"
	navIndexTitle    = "Overview"
)

// navEntry is one "Title: path" item of an mkdocs nav list.
type navEntry struct{ title, path string }

// TestMkdocsNavListsEveryGeneratedPage ties the hand-written mkdocs nav to the
// generator. mkdocs --strict only logs INFO for a page missing from nav, so a
// new or renamed family page would otherwise ship unreachable from the sidebar.
// The nav must list the index first, then every page in layout order, each
// under the title pageLayout gives it.
func TestMkdocsNavListsEveryGeneratedPage(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), filepath.FromSlash(mkdocsRelPath)))
	if err != nil {
		t.Fatalf("read %s: %v", mkdocsRelPath, err)
	}
	nav := decodeNav(t, data)
	got, ok := findNavSection(nav, navSectionTitle)
	if !ok {
		t.Fatalf("%s has no nav section %q", mkdocsRelPath, navSectionTitle)
	}

	pages := mustRenderPages(t, Default())
	want := []navEntry{{navIndexTitle, navReferencePath + pages[0].Path}}
	for i, spec := range pageLayout {
		want = append(want, navEntry{spec.Title, navReferencePath + pages[i+1].Path})
	}
	if len(got) != len(want) {
		t.Fatalf("nav section %q has %d entries, want %d (index + every generated page): got %v, want %v",
			navSectionTitle, len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("nav entry %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// decodeNav decodes only the top-level "nav" key. mkdocs.yml repeats the
// markdown_extensions key, which yaml.v3 rejects when decoding the whole file
// into a map or struct, so the document is parsed as a node tree first.
func decodeNav(t *testing.T, data []byte) []any {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", mkdocsRelPath, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		t.Fatalf("%s is not a YAML mapping", mkdocsRelPath)
	}
	top := doc.Content[0].Content
	for i := 0; i+1 < len(top); i += 2 {
		if top[i].Value != "nav" {
			continue
		}
		var nav []any
		if err := top[i+1].Decode(&nav); err != nil {
			t.Fatalf("decode %s nav: %v", mkdocsRelPath, err)
		}
		return nav
	}
	t.Fatalf("%s has no nav key", mkdocsRelPath)
	return nil
}

// findNavSection returns the children of the nav section titled title, searching
// nested sections.
func findNavSection(nav []any, title string) ([]navEntry, bool) {
	for _, item := range nav {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for key, value := range m {
			children, isList := value.([]any)
			if !isList {
				continue
			}
			if key == title {
				return navEntries(children), true
			}
			if found, ok := findNavSection(children, title); ok {
				return found, true
			}
		}
	}
	return nil, false
}

func navEntries(items []any) []navEntry {
	var out []navEntry
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for title, value := range m {
			if path, isPath := value.(string); isPath {
				out = append(out, navEntry{title, path})
			}
		}
	}
	return out
}
