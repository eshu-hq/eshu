package skillgen

import (
	"fmt"
	"strings"
)

// cursorAdapter renders the .cursor/rules/eshu.mdc file the Cursor loader
// expects. The .mdc file IS the always-on layer; Cursor has no separate
// CLAUDE.md-style file, and the S1 design names the .mdc as the host's
// always-on mechanism. The frontmatter uses Cursor's rule schema
// (description, globs, alwaysApply) — the "name" frontmatter field is
// replaced by the rule id (the file name).
//
// The YAML frontmatter is at byte 0; the byte-citation block follows
// after. Cursor's loader discovers rules from the leading `---` block,
// so frontmatter-at-byte-0 is required for the rule to apply.
type cursorAdapter struct{}

func (cursorAdapter) Host() Host { return HostCursor }

func (cursorAdapter) OutputPath() string { return ".cursor/rules/eshu.mdc" }

func (a cursorAdapter) Render(in RenderInput) ([]byte, error) {
	commentBlock, err := normalizeCommentBlock(in.CommentBlock, in.Fragments)
	if err != nil {
		return nil, fmt.Errorf("cursor adapter: %w", err)
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("description: |\n")
	for _, line := range wrapDescription(combinedDescription(in.Fragments), 72) {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("globs: \n")
	b.WriteString("alwaysApply: true\n")
	b.WriteString("---\n\n")
	if commentBlock != "" {
		b.WriteString(commentBlock)
		b.WriteString("\n\n")
	}
	b.WriteString("# Eshu Agent Rule (eshu)\n\n")
	b.WriteString("This rule is auto-generated from `skill-fragments/`. Do not edit it by hand; run `go run ./cmd/skillgen gen` to regenerate.\n\n")
	for _, fragment := range in.Fragments {
		b.WriteString(fragmentSection(fragment, in.Capabilities))
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}
