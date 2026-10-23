// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package envregistry

import (
	"fmt"
	"strings"
	"testing"
)

// pageLineCount counts lines the way scripts/lib/markdown-line-cap-core.sh
// does (awk NR, one per record), so these tests and the CI line-cap gate agree.
func pageLineCount(content string) int {
	return strings.Count(content, "\n")
}

func mustRenderPages(t *testing.T, r *Registry) []Page {
	t.Helper()
	pages, err := r.RenderPages()
	if err != nil {
		t.Fatalf("RenderPages: %v", err)
	}
	return pages
}

// TestRenderPagesEmitsIndexAndSubsystemPages pins the shape: the stable
// env-registry.md index first, then one page per layout entry in layout order.
func TestRenderPagesEmitsIndexAndSubsystemPages(t *testing.T) {
	t.Parallel()
	pages := mustRenderPages(t, Default())
	if len(pages) != len(pageLayout)+1 {
		t.Fatalf("RenderPages returned %d pages, want %d (index + layout)", len(pages), len(pageLayout)+1)
	}
	if pages[0].Path != IndexPagePath {
		t.Fatalf("first page = %q, want the index %q", pages[0].Path, IndexPagePath)
	}
	for i, spec := range pageLayout {
		want := PagesDir + "/" + spec.Slug + ".md"
		if pages[i+1].Path != want {
			t.Fatalf("page %d path = %q, want %q", i+1, pages[i+1].Path, want)
		}
	}
}

// TestRenderPagesEachUnderLineCap is the acceptance criterion: no generated
// page, index included, reaches the Markdown line cap.
func TestRenderPagesEachUnderLineCap(t *testing.T) {
	t.Parallel()
	for _, p := range mustRenderPages(t, Default()) {
		if n := pageLineCount(p.Content); n > PageLineCap {
			t.Errorf("%s has %d lines, over the %d-line cap", p.Path, n, PageLineCap)
		}
	}
}

// TestRenderPagesCarriesEveryEntryExactlyOnce proves the split loses and
// duplicates nothing: each registry variable has exactly one table row across
// the subsystem pages.
func TestRenderPagesCarriesEveryEntryExactlyOnce(t *testing.T) {
	t.Parallel()
	r := Default()
	pages := mustRenderPages(t, r)
	for _, e := range r.Entries() {
		row := "| `" + e.Name + "` |"
		hits := 0
		for _, p := range pages[1:] {
			hits += strings.Count(p.Content, row)
		}
		if hits != 1 {
			t.Errorf("%s has %d table rows across subsystem pages, want 1", e.Name, hits)
		}
	}
}

func TestRenderPagesIsDeterministic(t *testing.T) {
	t.Parallel()
	first := mustRenderPages(t, Default())
	second := mustRenderPages(t, Default())
	if len(first) != len(second) {
		t.Fatalf("page counts differ: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("page %s differs between renders", first[i].Path)
		}
	}
}

// TestRenderPagesFormatInvariants keeps the whitespace gate and the
// generated-marker contract on every page, not just the index.
func TestRenderPagesFormatInvariants(t *testing.T) {
	t.Parallel()
	marker := "Generated from go/internal/envregistry. Do not edit by hand"
	wrapper := "regenerate with `bash scripts/generate-env-registry-doc.sh`"
	for _, p := range mustRenderPages(t, Default()) {
		if !strings.HasSuffix(p.Content, "\n") || strings.HasSuffix(p.Content, "\n\n") {
			t.Errorf("%s must end with exactly one newline", p.Path)
		}
		if strings.Contains(p.Content, "\r") {
			t.Errorf("%s contains a carriage return", p.Path)
		}
		if !strings.Contains(p.Content, marker) || !strings.Contains(p.Content, wrapper) {
			t.Errorf("%s is missing the generated marker or wrapper command", p.Path)
		}
		if strings.Contains(p.Content, "ESHU_UPDATE_ENV_DOC=1 go test") {
			t.Errorf("%s exposes the raw update-only test command", p.Path)
		}
	}
}

// TestIndexLinksEverySubsystemPageAndAnchor keeps the stable env-registry.md
// URL useful: it must reach every page and every subsystem section.
func TestIndexLinksEverySubsystemPageAndAnchor(t *testing.T) {
	t.Parallel()
	index := mustRenderPages(t, Default())[0].Content
	for _, spec := range pageLayout {
		link := "(" + PagesDir + "/" + spec.Slug + ".md)"
		if !strings.Contains(index, link) {
			t.Errorf("index does not link page %s", link)
		}
		for _, subsystem := range spec.Subsystems {
			anchor := "(" + PagesDir + "/" + spec.Slug + ".md#" + subsystem + ")"
			if !strings.Contains(index, anchor) {
				t.Errorf("index does not link subsystem anchor %s", anchor)
			}
		}
	}
}

// TestRenderPagesBoundaryAtLineCap proves the cap is inclusive: a page of
// exactly the cap renders, one line over is refused.
func TestRenderPagesBoundaryAtLineCap(t *testing.T) {
	t.Parallel()
	r := Default()
	longest := 0
	for _, p := range mustRenderPages(t, r) {
		longest = max(longest, pageLineCount(p.Content))
	}
	if _, err := r.renderPages(pageLayout, longest); err != nil {
		t.Fatalf("a cap equal to the longest page (%d) must pass: %v", longest, err)
	}
	if _, err := r.renderPages(pageLayout, longest-1); err == nil {
		t.Fatalf("a cap one below the longest page (%d) must fail", longest-1)
	}
}

// layoutCoveringEntries returns one entry per subsystem named in pageLayout, so
// a synthetic registry satisfies the layout and a test isolates one defect.
func layoutCoveringEntries() []Entry {
	var entries []Entry
	for _, spec := range pageLayout {
		for _, subsystem := range spec.Subsystems {
			entries = append(entries, Entry{
				Name: "ESHU_LAYOUT_" + strings.ToUpper(strings.ReplaceAll(subsystem, "-", "_")),
				Type: VarString, Subsystem: subsystem, Description: "layout filler",
			})
		}
	}
	return entries
}

// TestRenderPagesSeededOverCapFails is the seeded violation: enough entries in
// one subsystem push its page past the real 500-line cap, and the generator
// names the page and says what to do instead of writing it.
func TestRenderPagesSeededOverCapFails(t *testing.T) {
	t.Parallel()
	entries := layoutCoveringEntries()
	for i := range 500 {
		entries = append(entries, Entry{
			Name: fmt.Sprintf("ESHU_SEEDED_%03d", i), Type: VarString,
			Subsystem: "api", Description: "seeded entry",
		})
	}
	r, err := New(entries)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = r.RenderPages()
	if err == nil {
		t.Fatal("RenderPages must fail when a page exceeds the line cap")
	}
	for _, want := range []string{"env-registry/api-and-auth.md", "500", "pageLayout"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q must mention %q", err, want)
		}
	}
}

// TestRenderPagesUnassignedSubsystemFails: a subsystem the layout does not
// name must fail loudly rather than vanish from the published reference.
func TestRenderPagesUnassignedSubsystemFails(t *testing.T) {
	t.Parallel()
	r, err := New(append(layoutCoveringEntries(),
		Entry{Name: "ESHU_ORPHAN", Type: VarString, Subsystem: "brand-new", Description: "x"}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = r.RenderPages()
	if err == nil || !strings.Contains(err.Error(), "brand-new") {
		t.Fatalf("RenderPages error = %v, want one naming subsystem brand-new", err)
	}
}

// TestRenderPagesLayoutRejectsDuplicateAndStaleSubsystems guards the layout
// itself: a subsystem on two pages would publish its rows twice, and a layout
// subsystem with no entries is a stale mapping.
func TestRenderPagesLayoutRejectsDuplicateAndStaleSubsystems(t *testing.T) {
	t.Parallel()
	r := Default()
	dup := []pageSpec{
		{Slug: "a", Title: "A", Subsystems: []string{"api"}},
		{Slug: "b", Title: "B", Subsystems: []string{"api"}},
	}
	if _, err := r.renderPages(dup, PageLineCap); err == nil {
		t.Error("a subsystem on two pages must be rejected")
	}
	stale := append([]pageSpec{{Slug: "gone", Title: "Gone", Subsystems: []string{"no-such-subsystem"}}}, pageLayout...)
	if _, err := r.renderPages(stale, PageLineCap); err == nil {
		t.Error("a layout subsystem with no registry entries must be rejected")
	}
}
