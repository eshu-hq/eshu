// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package skillgen

import (
	"fmt"
	"strings"
)

// aiderAdapter renders Aider's conventions file.
//
// Aider is the one host in the matrix with no auto-discovered path: it loads a
// conventions file only when the operator names it, through `read:` in
// .aider.conf.yml or `--read` on the command line. Writing to a conventional
// root name such as CONVENTIONS.md would imply a discovery that does not
// happen, so the generated file lives under .aider/ and the wiring stays the
// operator's, in a file S2 does not regenerate. That config file is Aider's
// always-on layer in the sense the other rows use the term.
//
// Aider reads the file as plain prose. It defines no frontmatter schema, so
// this adapter emits none — a leading `---` block would be handed to the model
// as literal content rather than parsed. The byte-citation block is therefore
// the first thing in the file. It stays a Markdown comment, so it does not
// reach the model as instruction text while remaining the anchor S3 verifies.
type aiderAdapter struct{}

func (aiderAdapter) Host() Host { return HostAider }

func (aiderAdapter) OutputPath() string { return ".aider/eshu-conventions.md" }

func (a aiderAdapter) Render(in RenderInput) ([]byte, error) {
	commentBlock, err := normalizeCommentBlock(in.CommentBlock, in.Fragments)
	if err != nil {
		return nil, fmt.Errorf("aider adapter: %w", err)
	}
	var b strings.Builder
	if commentBlock != "" {
		b.WriteString(commentBlock)
		b.WriteString("\n\n")
	}
	b.WriteString("# Eshu Operating Standard\n\n")
	b.WriteString("These conventions are auto-generated from `skill-fragments/`. Do not edit them by hand; run `go run ./cmd/skillgen gen` to regenerate.\n\n")
	b.WriteString("Aider does not discover this file on its own. Point at it from `.aider.conf.yml`:\n\n")
	b.WriteString("```yaml\n")
	b.WriteString("read: .aider/eshu-conventions.md\n")
	b.WriteString("```\n\n")
	for _, fragment := range in.Fragments {
		b.WriteString(fragmentSection(fragment, in.Capabilities))
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}
