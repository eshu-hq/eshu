// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package skillgen

import (
	"fmt"
	"strings"
)

// geminiCLIAdapter renders the Gemini CLI workspace context file.
//
// Gemini CLI loads context files hierarchically — user home, then workspace
// and its parents, then per-directory files a tool touches — and concatenates
// everything it finds into every prompt. There is no separate skill layer, so
// this row behaves like Cursor: the generated file IS the always-on layer
// rather than an augmentation of one.
//
// GEMINI.md is the default name; a deployment that has repointed
// contextFileName in .gemini/settings.json is renaming this file, not opting
// out of it. The format is plain Markdown with no frontmatter schema, so the
// byte-citation block leads the file as a comment: Gemini would otherwise
// forward a `---` block to the model as literal content.
//
// Because the loader concatenates rather than replaces, this file is additive
// to whatever a contributor keeps in a home-level GEMINI.md.
type geminiCLIAdapter struct{}

func (geminiCLIAdapter) Host() Host { return HostGeminiCLI }

func (geminiCLIAdapter) OutputPath() string { return "GEMINI.md" }

func (a geminiCLIAdapter) Render(in RenderInput) ([]byte, error) {
	commentBlock, err := normalizeCommentBlock(in.CommentBlock, in.Fragments)
	if err != nil {
		return nil, fmt.Errorf("gemini-cli adapter: %w", err)
	}
	var b strings.Builder
	if commentBlock != "" {
		b.WriteString(commentBlock)
		b.WriteString("\n\n")
	}
	b.WriteString("# Eshu Operating Standard\n\n")
	b.WriteString("This context file is auto-generated from `skill-fragments/`. Do not edit it by hand; run `go run ./cmd/skillgen gen` to regenerate.\n\n")
	for _, fragment := range in.Fragments {
		b.WriteString(fragmentSection(fragment, in.Capabilities))
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}
