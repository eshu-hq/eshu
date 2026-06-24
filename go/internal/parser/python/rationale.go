// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package python

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// pythonIntentCommentKinds are the intent-comment markers turned into rationale
// (issue #2230). A leading `# WHY:`/`# HACK:`/`# NOTE:`/`# TODO:`/`# FIXME:`
// comment that precedes a function or class explains design intent.
var pythonIntentCommentKinds = []string{"WHY", "HACK", "NOTE", "TODO", "FIXME"}

// pythonRationaleComments returns the intent comments that immediately precede a
// function or class declaration, in source order. Decorated definitions anchor
// on the decorated_definition wrapper so the comment above the decorator is
// found. Only contiguous leading comments are considered, so an unrelated
// comment elsewhere in the block does not attach.
func pythonRationaleComments(node *tree_sitter.Node, source []byte) []map[string]any {
	if node == nil {
		return nil
	}
	anchor := node
	if parent := node.Parent(); parent != nil && parent.Kind() == "decorated_definition" {
		anchor = parent
	}

	rationale := make([]map[string]any, 0)
	// Only comments directly above the declaration, with no blank-line gap,
	// attach. Tree-sitter does not represent blank lines, so adjacency is
	// enforced by line number: each comment must end on the line immediately
	// above the next accepted node.
	expectedEndLine := nodeLine(anchor) - 1
	for sibling := anchor.PrevSibling(); sibling != nil; sibling = sibling.PrevSibling() {
		if sibling.Kind() != "comment" || nodeEndLine(sibling) != expectedEndLine {
			break
		}
		expectedEndLine = nodeLine(sibling) - 1
		kind, body := pythonMatchIntentComment(nodeText(sibling, source))
		if kind == "" {
			continue
		}
		rationale = append(rationale, map[string]any{"kind": kind, "text": body})
	}
	if len(rationale) == 0 {
		return nil
	}
	// Siblings are walked backward; reverse to source order.
	for left, right := 0, len(rationale)-1; left < right; left, right = left+1, right-1 {
		rationale[left], rationale[right] = rationale[right], rationale[left]
	}
	return rationale
}

func pythonMatchIntentComment(raw string) (string, string) {
	text := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(raw), "#"))
	for _, kind := range pythonIntentCommentKinds {
		if strings.HasPrefix(text, kind+":") {
			return kind, strings.TrimSpace(text[len(kind)+1:])
		}
	}
	return "", ""
}
