// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"
)

// Managed-block markers delimit the Eshu-owned region inside a project
// instruction file. Reinstall replaces only the bytes between these markers,
// and uninstall removes the markers and their contents. All other file content
// is preserved verbatim. The markers are HTML comments so they are inert in
// Markdown files (CLAUDE.md, AGENTS.md) and ignored by Cursor MDC rendering.
const (
	guidanceBeginMarker = "<!-- BEGIN ESHU GUIDANCE -->"
	guidanceEndMarker   = "<!-- END ESHU GUIDANCE -->"
)

// blockStatus describes the state of the Eshu managed block within a file.
type blockStatus int

const (
	// blockAbsent means the file has no Eshu managed block.
	blockAbsent blockStatus = iota
	// blockCurrent means the managed block exists and matches the desired body.
	blockCurrent
	// blockStale means the managed block exists but its body differs from the
	// desired body, so a reinstall would rewrite it.
	blockStale
)

// renderManagedBlock wraps body in the begin/end markers, producing the exact
// bytes that live inside a managed region. The body is trimmed of surrounding
// blank lines so repeated installs are byte-stable regardless of incidental
// whitespace in the generated content.
func renderManagedBlock(body string) string {
	trimmed := strings.Trim(body, "\n")
	return guidanceBeginMarker + "\n" + trimmed + "\n" + guidanceEndMarker
}

// findManagedBlock locates the managed region in content. It returns the byte
// offsets of the block start (at the begin marker) and end (just past the end
// marker), plus whether a block was found. A malformed file with a begin marker
// but no following end marker reports found=false so callers treat it as absent
// and append a fresh block rather than corrupting the file.
func findManagedBlock(content string) (start, end int, found bool) {
	start = strings.Index(content, guidanceBeginMarker)
	if start < 0 {
		return 0, 0, false
	}
	endMarkerIdx := strings.Index(content[start:], guidanceEndMarker)
	if endMarkerIdx < 0 {
		return 0, 0, false
	}
	end = start + endMarkerIdx + len(guidanceEndMarker)
	return start, end, true
}

// extractManagedBody returns the body inside the managed block (without the
// markers) and whether a block was present. The returned body is trimmed of
// surrounding newlines so it can be compared directly against renderManagedBlock
// input.
func extractManagedBody(content string) (string, bool) {
	start, end, found := findManagedBlock(content)
	if !found {
		return "", false
	}
	inner := content[start+len(guidanceBeginMarker) : end-len(guidanceEndMarker)]
	return strings.Trim(inner, "\n"), true
}

// classifyBlock reports whether content already carries a managed block and
// whether that block matches the desired body.
func classifyBlock(content, desiredBody string) blockStatus {
	body, found := extractManagedBody(content)
	if !found {
		return blockAbsent
	}
	if body == strings.Trim(desiredBody, "\n") {
		return blockCurrent
	}
	return blockStale
}

// upsertManagedBlock returns content with the managed block set to body. If the
// file already has a managed block, only the bytes between the markers are
// replaced and every other byte (before and after the block) is preserved
// exactly. If no block exists, a new one is appended after the existing content
// with a single blank-line separator. The operation is idempotent: applying it
// twice with the same body yields identical output.
func upsertManagedBlock(content, body string) string {
	rendered := renderManagedBlock(body)
	start, end, found := findManagedBlock(content)
	if found {
		return content[:start] + rendered + content[end:]
	}
	if strings.TrimSpace(content) == "" {
		return rendered + "\n"
	}
	// Separate the appended block from prior content with exactly one blank
	// line, regardless of the trailing whitespace already present.
	prefix := strings.TrimRight(content, "\n")
	return prefix + "\n\n" + rendered + "\n"
}

// removeManagedBlock returns content with the managed block (and its markers)
// removed, preserving all surrounding text. The second return value reports
// whether a block was present and removed. Blank lines that bracketed the block
// are collapsed so removal does not leave a growing gap, but text before and
// after the block is otherwise untouched.
func removeManagedBlock(content string) (string, bool) {
	start, end, found := findManagedBlock(content)
	if !found {
		return content, false
	}
	before := content[:start]
	after := content[end:]

	// Collapse the seam: drop trailing newlines from the prefix and leading
	// newlines from the suffix, then rejoin with a single newline only when
	// both sides have content.
	trimmedBefore := strings.TrimRight(before, "\n")
	trimmedAfter := strings.TrimLeft(after, "\n")

	switch {
	case trimmedBefore == "" && trimmedAfter == "":
		return "", true
	case trimmedBefore == "":
		return trimmedAfter, true
	case trimmedAfter == "":
		// Preserve a single trailing newline so the file stays POSIX-clean.
		return trimmedBefore + "\n", true
	default:
		return trimmedBefore + "\n\n" + trimmedAfter, true
	}
}

// managedBlockSummary renders a short human description of a block status for
// status output.
func managedBlockSummary(status blockStatus) string {
	switch status {
	case blockCurrent:
		return "current"
	case blockStale:
		return "out-of-date"
	case blockAbsent:
		return "not installed"
	default:
		return fmt.Sprintf("unknown(%d)", int(status))
	}
}
