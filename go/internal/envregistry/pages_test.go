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

// renderFailureCase is one way the generator must refuse to render.
type renderFailureCase struct {
	name    string
	reg     *Registry
	layout  []pageSpec
	lineCap int
	wantErr []string
}

// renderFailureCases covers every refusal class once, each on a registry or
// layout that is otherwise valid so a case isolates exactly one defect.
func renderFailureCases(t *testing.T) []renderFailureCase {
	t.Helper()
	must := func(entries []Entry) *Registry {
		r, err := New(entries)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return r
	}
	base := must(layoutCoveringEntries())
	oversize := layoutCoveringEntries()
	for i := range 500 {
		oversize = append(oversize, Entry{
			Name: fmt.Sprintf("ESHU_SEEDED_%03d", i), Type: VarString,
			Subsystem: "api", Description: "seeded entry",
		})
	}
	withSub := func(subsystem string) *Registry {
		return must(append(layoutCoveringEntries(),
			Entry{Name: "ESHU_EXTRA", Type: VarString, Subsystem: subsystem, Description: "x"}))
	}
	oneSpec := func(spec pageSpec) []pageSpec { return append(append([]pageSpec(nil), pageLayout...), spec) }

	return []renderFailureCase{
		{
			"over the line cap", must(oversize), pageLayout, PageLineCap,
			[]string{"env-registry/api-and-auth.md", "500", "pageLayout"},
		},
		{"unassigned subsystem", withSub("brand-new"), pageLayout, PageLineCap, []string{"brand-new"}},
		{"empty subsystem", withSub(""), pageLayout, PageLineCap, []string{"empty subsystem"}},
		{"subsystem with an unsafe anchor name", withSub("Foo Bar"), oneSpec(
			pageSpec{Slug: "odd", Title: "Odd", Subsystems: []string{"Foo Bar"}}), PageLineCap, []string{"Foo Bar"}},
		{"subsystem on two pages", base, []pageSpec{
			{Slug: "a", Title: "A", Subsystems: []string{"api"}},
			{Slug: "b", Title: "B", Subsystems: []string{"api"}},
		}, PageLineCap, []string{"api", "both"}},
		{
			"layout subsystem with no entries", base, oneSpec(
				pageSpec{Slug: "gone", Title: "Gone", Subsystems: []string{"no-such-subsystem"}}), PageLineCap,
			[]string{"no-such-subsystem"},
		},
		{"duplicate slug", base, oneSpec(
			pageSpec{Slug: pageLayout[0].Slug, Title: "Dup"}), PageLineCap, []string{pageLayout[0].Slug}},
		{"empty slug", base, oneSpec(pageSpec{Slug: "", Title: "No slug"}), PageLineCap, []string{"slug"}},
		{"index slug", base, oneSpec(pageSpec{Slug: "index", Title: "Index"}), PageLineCap, []string{"index"}},
		{
			"slug that is not a URL-safe name", base, oneSpec(pageSpec{Slug: "Bad Slug", Title: "Bad"}), PageLineCap,
			[]string{"Bad Slug"},
		},
		{"empty title", base, oneSpec(pageSpec{Slug: "untitled"}), PageLineCap, []string{"untitled", "title"}},
	}
}

// TestRenderPagesRefusesEveryFailureClass proves each refusal returns an error
// naming the defect and returns no pages at all, never a partial set.
func TestRenderPagesRefusesEveryFailureClass(t *testing.T) {
	t.Parallel()
	for _, tc := range renderFailureCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pages, err := tc.reg.renderPages(tc.layout, tc.lineCap)
			if err == nil {
				t.Fatal("renderPages must fail")
			}
			if pages != nil {
				t.Fatalf("renderPages returned %d page(s) alongside an error, want none", len(pages))
			}
			for _, want := range tc.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q must mention %q", err, want)
				}
			}
		})
	}
}

// TestIndexExplainsMovedSubsystemAnchors: links like env-registry.md#api that
// predate the split now land at the top of the index, which has no subsystem
// headings. The index must say so and point at the table that finds each one.
func TestIndexExplainsMovedSubsystemAnchors(t *testing.T) {
	t.Parallel()
	index := mustRenderPages(t, Default())[0].Content
	for _, want := range []string{"`#api`", "moved to the family pages", "## Subsystems"} {
		if !strings.Contains(index, want) {
			t.Errorf("index must contain %q", want)
		}
	}
}
