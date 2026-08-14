// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package assistantguidance

import (
	"fmt"
	"strings"
)

// Managed-block markers delimit the Eshu-owned region inside a project
// instruction file. Reinstall replaces only the bytes between these markers,
// and uninstall removes the markers and their contents. All other file content
// is preserved verbatim. The markers are HTML comments so they are inert in
// Markdown files (CLAUDE.md, AGENTS.md) and ignored by Cursor MDC rendering.
//
// The marker pair is the block's ONLY identity: an exact pair anywhere in the
// file -- including inside a fenced code block that happens to quote it -- is
// the managed region. A near miss (different spacing, different case, extra
// words) is not a block and is left alone.
const (
	BeginMarker = "<!-- BEGIN ESHU GUIDANCE -->"
	EndMarker   = "<!-- END ESHU GUIDANCE -->"
)

// BlockStatus describes the state of the Eshu managed block within a file.
type BlockStatus int

const (
	// BlockAbsent means the file has no Eshu managed block.
	BlockAbsent BlockStatus = iota
	// BlockCurrent means the managed block exists and matches the desired body.
	BlockCurrent
	// BlockStale means the managed block exists but its body differs from the
	// desired body, so a reinstall would rewrite it.
	BlockStale
)

// RenderBlock wraps body in the begin/end markers, producing the exact bytes
// that live inside a managed region. The body is trimmed of surrounding blank
// lines so repeated installs are byte-stable regardless of incidental
// whitespace in the generated content. The markers and the separating newlines
// are always LF, whatever line endings the surrounding file uses.
func RenderBlock(body string) string {
	trimmed := strings.Trim(body, "\n")
	return BeginMarker + "\n" + trimmed + "\n" + EndMarker
}

// FindBlock locates the managed region in content. It returns the byte offsets
// of the block start (at the begin marker) and end (just past the end marker),
// plus whether a block was found. A malformed file with a begin marker but no
// following end marker reports found=false so callers treat it as absent and
// append a fresh block rather than corrupting the file.
func FindBlock(content string) (start, end int, found bool) {
	start = strings.Index(content, BeginMarker)
	if start < 0 {
		return 0, 0, false
	}
	endMarkerIdx := strings.Index(content[start:], EndMarker)
	if endMarkerIdx < 0 {
		return 0, 0, false
	}
	end = start + endMarkerIdx + len(EndMarker)
	return start, end, true
}

// ExtractBody returns the body inside the managed block (without the markers)
// and whether a block was present. The returned body is trimmed of surrounding
// newlines so it can be compared directly against RenderBlock input.
func ExtractBody(content string) (string, bool) {
	start, end, found := FindBlock(content)
	if !found {
		return "", false
	}
	inner := content[start+len(BeginMarker) : end-len(EndMarker)]
	return strings.Trim(inner, "\n"), true
}

// Classify reports whether content already carries a managed block and whether
// that block matches the desired body.
func Classify(content, desiredBody string) BlockStatus {
	body, found := ExtractBody(content)
	if !found {
		return BlockAbsent
	}
	if body == strings.Trim(desiredBody, "\n") {
		return BlockCurrent
	}
	return BlockStale
}

// Upsert returns content with the managed block set to body. If the file
// already has a managed block, only the bytes between the markers are replaced
// and every other byte (before and after the block) is preserved exactly. If no
// block exists, a new one is appended after the existing content with a single
// blank-line separator. The operation is idempotent: applying it twice with the
// same body yields identical output.
func Upsert(content, body string) string {
	rendered := RenderBlock(body)
	start, end, found := FindBlock(content)
	if found {
		return content[:start] + rendered + content[end:]
	}
	if strings.TrimSpace(content) == "" {
		return rendered + "\n"
	}
	// Separate the appended block from prior content with exactly one blank
	// line, regardless of the trailing whitespace already present. TrimRight
	// only strips LF, so a CRLF file keeps its final carriage return.
	prefix := strings.TrimRight(content, "\n")
	return prefix + "\n\n" + rendered + "\n"
}

// Remove returns content with the managed block (and its markers) removed,
// preserving all surrounding text. The second return value reports whether a
// block was present and removed. Blank lines that bracketed the block are
// collapsed so removal does not leave a growing gap, but text before and after
// the block is otherwise untouched.
func Remove(content string) (string, bool) {
	start, end, found := FindBlock(content)
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

// BlockSummary renders a short human description of a block status for status
// output.
func BlockSummary(status BlockStatus) string {
	switch status {
	case BlockCurrent:
		return "current"
	case BlockStale:
		return "out-of-date"
	case BlockAbsent:
		return "not installed"
	default:
		return fmt.Sprintf("unknown(%d)", int(status))
	}
}
