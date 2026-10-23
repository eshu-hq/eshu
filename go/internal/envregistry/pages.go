// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package envregistry

import (
	"fmt"
	"sort"
	"strings"
)

const (
	// PageLineCap is the repository's Markdown file cap
	// (scripts/verify-markdown-line-cap.sh). RenderPages refuses to produce a
	// page over it, so the line-cap gate is never the first place an oversized
	// reference page is noticed.
	PageLineCap = 500

	// IndexPagePath is the stable, linkable entry point of the reference,
	// relative to docs/public/reference. Existing docs link to it.
	IndexPagePath = "env-registry.md"

	// PagesDir is the directory under docs/public/reference that holds the
	// per-family pages. It deliberately avoids the path segment "generated":
	// the line-cap gate skips that segment, which would hide an oversized page.
	PagesDir = "env-registry"
)

// Page is one generated Markdown file of the reference.
type Page struct {
	// Path is relative to docs/public/reference and uses forward slashes.
	Path string
	// Content is the full file body, ending in exactly one newline.
	Content string
}

// pageSpec assigns a family of subsystems to one published page.
type pageSpec struct {
	// Slug is the page file name without extension; it is part of a public URL.
	Slug string
	// Title is the page's human name.
	Title string
	// Summary is one sentence describing what the page covers.
	Summary string
	// Subsystems lists the Entry.Subsystem values rendered on this page.
	Subsystems []string
}

// pageLayout is the explicit subsystem-to-page assignment. The registry has no
// grouping field beyond Subsystem, so the family grouping lives here. It is
// explicit rather than packed greedily so a page's URL and anchors never move
// when a variable is added. A subsystem missing here, listed twice, or listed
// without entries fails the generator. To rebalance a page that nears
// PageLineCap, move whole subsystems between entries; target 350 lines or
// fewer so future variables do not trigger it again.
var pageLayout = []pageSpec{
	{
		Slug: "api-and-auth", Title: "API, auth, and MCP",
		Summary:    "Listen addresses, authentication, browser sessions, OIDC and GitHub login, and the MCP server.",
		Subsystems: []string{"api", "auth", "mcp"},
	},
	{
		Slug: "runtime-and-storage", Title: "Runtime and storage",
		Summary:    "Shared runtime settings, Postgres, the graph backend, and backend conformance test knobs.",
		Subsystems: []string{"backend-conformance", "graph", "postgres", "runtime"},
	},
	{
		Slug: "reducer-and-projection", Title: "Reducer and projection",
		Summary:    "The reducer, projector, workflow coordinator, push hooks, and semantic providers.",
		Subsystems: []string{"coordinator", "projector", "push-hooks", "reducer", "semantic"},
	},
	{
		Slug: "collectors-platform", Title: "Ingester and collector framework",
		Summary:    "The ingester's git collector, the component package manager, and the component-extension collector.",
		Subsystems: []string{"collector", "collector-component-extension", "component"},
	},
	{
		Slug: "collectors-infrastructure", Title: "Infrastructure and registry collectors",
		Summary: "Cloud, Kubernetes, Vault, Terraform state, OCI registry, and package registry collectors.",
		Subsystems: []string{
			"collector-aws-cloud", "collector-azure-cloud", "collector-gcp-cloud",
			"collector-kubernetes-live", "collector-oci-registry", "collector-package-registry",
			"collector-terraform-state", "collector-vault-live",
		},
	},
	{
		Slug: "collectors-signals", Title: "Observability, delivery, and security collectors",
		Summary: "Grafana, Loki, Prometheus/Mimir, Tempo, PagerDuty, Jira, CI/CD run, security alert, SBOM, and vulnerability collectors.",
		Subsystems: []string{
			"collector-cicd-run", "collector-grafana", "collector-jira", "collector-loki",
			"collector-pagerduty", "collector-prometheus-mimir", "collector-sbom-attestation",
			"collector-security-alerts", "collector-tempo", "collector-vulnerability-intelligence",
		},
	},
}

const generatedMarker = "<!-- Generated from go/internal/envregistry. Do not edit by hand; " +
	"regenerate with `bash scripts/generate-env-registry-doc.sh`. -->\n\n"

// RenderPages renders the committed reference: the index page first, then one
// page per pageLayout entry in layout order. Output is deterministic. It
// returns an error, and no pages, when a subsystem is unassigned, assigned
// twice, assigned without entries, or when any page would exceed PageLineCap.
func (r *Registry) RenderPages() ([]Page, error) {
	return r.renderPages(pageLayout, PageLineCap)
}

func (r *Registry) renderPages(layout []pageSpec, lineCap int) ([]Page, error) {
	bySubsystem := map[string][]Entry{}
	for _, e := range r.Entries() {
		bySubsystem[e.Subsystem] = append(bySubsystem[e.Subsystem], e)
	}
	pageOf, err := assignSubsystems(layout, bySubsystem)
	if err != nil {
		return nil, err
	}

	pages := make([]Page, 0, len(layout)+1)
	pages = append(pages, Page{Path: IndexPagePath, Content: renderIndex(layout, bySubsystem, pageOf)})
	for _, spec := range layout {
		pages = append(pages, Page{
			Path:    PagesDir + "/" + spec.Slug + ".md",
			Content: renderFamilyPage(spec, bySubsystem),
		})
	}
	for _, p := range pages {
		if n := strings.Count(p.Content, "\n"); n > lineCap {
			return nil, fmt.Errorf("envregistry: %s would be %d lines, over the %d-line Markdown cap; "+
				"move whole subsystems to another page in pageLayout (go/internal/envregistry/pages.go) "+
				"or add a page", p.Path, n, lineCap)
		}
	}
	return pages, nil
}

// assignSubsystems validates layout against the registry's subsystems and
// returns the page spec index owning each subsystem.
func assignSubsystems(layout []pageSpec, bySubsystem map[string][]Entry) (map[string]int, error) {
	pageOf := map[string]int{}
	slugs := map[string]struct{}{}
	for i, spec := range layout {
		if _, dup := slugs[spec.Slug]; dup {
			return nil, fmt.Errorf("envregistry: page slug %q appears twice in pageLayout", spec.Slug)
		}
		slugs[spec.Slug] = struct{}{}
		for _, subsystem := range spec.Subsystems {
			if prev, dup := pageOf[subsystem]; dup {
				return nil, fmt.Errorf("envregistry: subsystem %q is on both page %q and page %q in pageLayout",
					subsystem, layout[prev].Slug, spec.Slug)
			}
			if len(bySubsystem[subsystem]) == 0 {
				return nil, fmt.Errorf("envregistry: pageLayout page %q lists subsystem %q, which has no registry entries",
					spec.Slug, subsystem)
			}
			pageOf[subsystem] = i
		}
	}
	var missing []string
	for subsystem := range bySubsystem {
		if _, ok := pageOf[subsystem]; !ok {
			missing = append(missing, subsystem)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("envregistry: subsystem(s) %s have registry entries but no page in pageLayout "+
			"(go/internal/envregistry/pages.go); assign each to a page", strings.Join(missing, ", "))
	}
	return pageOf, nil
}

func renderFamilyPage(spec pageSpec, bySubsystem map[string][]Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Environment Variables: %s\n\n", spec.Title)
	b.WriteString(generatedMarker)
	fmt.Fprintf(&b, "%s This page is part of the [Environment Variable Reference](../%s), ", spec.Summary, IndexPagePath)
	b.WriteString("which lists every page. Run `eshu config validate` to check the current environment against the registry.\n\n")
	subsystems := append([]string(nil), spec.Subsystems...)
	sort.Strings(subsystems)
	for _, subsystem := range subsystems {
		writeSubsystemSection(&b, subsystem, bySubsystem[subsystem])
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func renderIndex(layout []pageSpec, bySubsystem map[string][]Entry, pageOf map[string]int) string {
	var b strings.Builder
	b.WriteString("# Environment Variable Reference\n\n")
	b.WriteString(generatedMarker)
	b.WriteString("This reference is generated from the code-owned registry in ")
	b.WriteString("`go/internal/envregistry`. It covers the core platform subsystems and the hosted collectors. ")
	b.WriteString("It is split into one page per subsystem family so each page stays under the repository's ")
	b.WriteString("Markdown line cap. Run `eshu config validate` to check the current environment against it.\n\n")

	b.WriteString("## Pages\n\n")
	b.WriteString("| Page | Covers | Variables |\n| --- | --- | --- |\n")
	for _, spec := range layout {
		count := 0
		for _, subsystem := range spec.Subsystems {
			count += len(bySubsystem[subsystem])
		}
		fmt.Fprintf(&b, "| [%s](%s/%s.md) | %s | %d |\n", spec.Title, PagesDir, spec.Slug, spec.Summary, count)
	}

	b.WriteString("\n## Subsystems\n\n")
	b.WriteString("| Subsystem | Variables | Page |\n| --- | --- | --- |\n")
	subsystems := make([]string, 0, len(pageOf))
	for subsystem := range pageOf {
		subsystems = append(subsystems, subsystem)
	}
	sort.Strings(subsystems)
	for _, subsystem := range subsystems {
		spec := layout[pageOf[subsystem]]
		fmt.Fprintf(&b, "| [`%s`](%s/%s.md#%s) | %d | %s |\n",
			subsystem, PagesDir, spec.Slug, subsystem, len(bySubsystem[subsystem]), spec.Title)
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
