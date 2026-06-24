// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package skillgen

import (
	"fmt"
	"sort"
	"strings"
)

// claudeCodeAdapter renders the .claude/skills/eshu/SKILL.md file the
// Claude Code loader expects. The YAML frontmatter is at byte 0 (the
// loader-safe position); the byte-citation block follows as an HTML
// comment so the loader reads the metadata first; each fragment body is
// appended under a "## <title>" heading derived from the fragment id.
//
// All three host adapters share this emission order: frontmatter at
// byte 0, citation block after. Claude Code's loader accepts the
// citation block before the frontmatter, but emitting frontmatter at
// byte 0 keeps the three hosts byte-comparable except for the
// host-specific schema fields and the always-on layer file.
//
// The adapter is the only place that knows the Claude Code frontmatter
// schema; adding a new frontmatter field is local to this file.
type claudeCodeAdapter struct{}

func (claudeCodeAdapter) Host() Host { return HostClaudeCode }

// OutputPath is .claude/skills/eshu/SKILL.md relative to the configured
// expected/ root. S2 joins it with the host's expected/ subdirectory.
func (claudeCodeAdapter) OutputPath() string { return ".claude/skills/eshu/SKILL.md" }

func (a claudeCodeAdapter) Render(in RenderInput) ([]byte, error) {
	commentBlock, err := normalizeCommentBlock(in.CommentBlock, in.Fragments)
	if err != nil {
		return nil, fmt.Errorf("claude-code adapter: %w", err)
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: eshu\n")
	b.WriteString("description: |\n")
	for _, line := range wrapDescription(combinedDescription(in.Fragments), 72) {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("---\n\n")
	if commentBlock != "" {
		b.WriteString(commentBlock)
		b.WriteString("\n\n")
	}
	b.WriteString("# Eshu Agent Skill\n\n")
	b.WriteString("This skill is auto-generated from `skill-fragments/`. Do not edit it by hand; run `go run ./cmd/skillgen gen` to regenerate.\n\n")
	for _, fragment := range in.Fragments {
		b.WriteString(fragmentSection(fragment, in.Capabilities))
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}

// combinedDescription joins the per-fragment description into a single
// frontmatter description. Fragments are already in id-sorted order, so
// the resulting description is stable.
func combinedDescription(fragments []Fragment) string {
	parts := make([]string, 0, len(fragments))
	for _, f := range fragments {
		parts = append(parts, f.Description)
	}
	return strings.Join(parts, " ")
}

// wrapDescription breaks a long description into lines of at most `width`
// runes, breaking on word boundaries. It is intentionally simple: a
// word-wrap that respects the frontmatter `|` literal block scalar, which
// the YAML loader preserves as-is.
func wrapDescription(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	lines := make([]string, 0, len(words))
	cur := words[0]
	for _, w := range words[1:] {
		if len(cur)+1+len(w) > width {
			lines = append(lines, cur)
			cur = w
			continue
		}
		cur = cur + " " + w
	}
	lines = append(lines, cur)
	return lines
}

// fragmentSection returns the rendered section for one fragment, including
// the H2 heading and the body. The per-collector-matrix fragment appends a
// deployment-specific tail.
func fragmentSection(fragment Fragment, caps Capabilities) string {
	var b strings.Builder
	b.WriteString("## ")
	b.WriteString(toTitle(fragment.ID))
	b.WriteString("\n\n")
	b.WriteString(strings.TrimRight(fragment.Body, "\n"))
	b.WriteString("\n")
	if fragment.ID == "per-collector-matrix" {
		b.WriteString("\n")
		b.WriteString(renderActiveCollectors(caps))
	}
	return b.String()
}

// toTitle converts "operating-standard" to "Operating Standard". The split
// is on "-" only; mixed-case ids are preserved.
func toTitle(id string) string {
	parts := strings.Split(id, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// normalizeCommentBlock ensures the byte-citation block is present and
// reflects the loaded fragments. It is a defensive pass: the render
// orchestrator is expected to have built the block from the same
// fragments, but a fragment whose citation becomes empty after a code
// change must not silently drop the comment line.
func normalizeCommentBlock(block string, fragments []Fragment) (string, error) {
	if block == "" {
		// The block is empty only when no fragments had a byte_citation;
		// every fragment must have one, so an empty block is a hard error.
		return "", fmt.Errorf("empty byte-citation block for %d fragments", len(fragments))
	}
	return block, nil
}

// renderActiveCollectors returns the per-deployment "Active Collectors on
// This Deployment" section. The list is sorted for deterministic output.
func renderActiveCollectors(caps Capabilities) string {
	var b strings.Builder
	b.WriteString("### Active Collectors on This Deployment\n\n")
	if caps.Source == "" {
		b.WriteString("The active collector set is the default set (all collectors enabled). Override by writing `skill-fragments/capabilities.local.yaml`.\n")
		return b.String()
	}
	b.WriteString("The active collector set on this deployment is enumerated below. Disabling a collector hides its MCP surface from the generated skill.\n\n")
	names := make([]string, 0, len(caps.Collectors))
	for name := range caps.Collectors {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		enabled := caps.Collectors[name]
		if enabled {
			b.WriteString("- ")
			b.WriteString(name)
			b.WriteString(" (enabled)\n")
		}
	}
	disabled := make([]string, 0)
	for _, name := range names {
		if !caps.Collectors[name] {
			disabled = append(disabled, name)
		}
	}
	if len(disabled) > 0 {
		b.WriteString("\nThe following collectors are disabled on this deployment and are not enumerated above:\n\n")
		for _, name := range disabled {
			b.WriteString("- ")
			b.WriteString(name)
			b.WriteString("\n")
		}
	}
	return b.String()
}
