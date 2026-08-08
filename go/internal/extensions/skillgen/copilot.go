// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package skillgen

import (
	"fmt"
	"strings"
)

// copilotAdapter renders the path-specific instructions file GitHub Copilot
// discovers under .github/instructions/. That directory is the mechanism with
// a documented frontmatter schema, so it is where a generated file belongs;
// the repository-wide .github/copilot-instructions.md is the always-on layer
// and stays hand-maintained, the same split Claude Code and Codex use with
// CLAUDE.md and AGENTS.md.
//
// applyTo is required by the loader and is the only frontmatter key Copilot
// documents for this file beyond the optional excludeAgent. The glob "**"
// makes the instructions apply repository-wide, matching how the other hosts
// load Eshu's guidance unconditionally. Copilot does not surface a name or a
// description field, so unlike Claude Code and Codex there is nowhere to put
// the fragment titles except the body.
//
// The frontmatter is at byte 0 and the byte-citation block follows it, which
// is the shape every adapter shares.
type copilotAdapter struct{}

func (copilotAdapter) Host() Host { return HostCopilot }

func (copilotAdapter) OutputPath() string { return ".github/instructions/eshu.instructions.md" }

func (a copilotAdapter) Render(in RenderInput) ([]byte, error) {
	commentBlock, err := normalizeCommentBlock(in.CommentBlock, in.Fragments)
	if err != nil {
		return nil, fmt.Errorf("copilot adapter: %w", err)
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("applyTo: \"**\"\n")
	b.WriteString("---\n\n")
	if commentBlock != "" {
		b.WriteString(commentBlock)
		b.WriteString("\n\n")
	}
	b.WriteString("# Eshu Operating Standard\n\n")
	b.WriteString("These instructions are auto-generated from `skill-fragments/`. Do not edit them by hand; run `go run ./cmd/skillgen gen` to regenerate.\n\n")
	for _, fragment := range in.Fragments {
		b.WriteString(fragmentSection(fragment, in.Capabilities))
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}
