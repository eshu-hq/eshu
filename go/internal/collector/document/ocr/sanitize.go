// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import (
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

const (
	// labelValueWindow is how many non-blank regions after a label count as
	// its value. Two is the smallest window that covers the shapes reading
	// order produces: two labels stacked above two values, because each label
	// restarts the window, and one caption line between a label and its value.
	// Every extra region hides one more line of ordinary text from a document,
	// so the window stays small and the row clause covers columns instead.
	labelValueWindow = 2

	// maxBareLabelWords caps a label with no ":" or "=" separator. "Confirm
	// password" is a form label; "Please rotate every staging password" is
	// prose, which keeps its own redaction but opens no window.
	maxBareLabelWords = 3

	// minLabelRowOverlap is the share of the shorter region's height that must
	// overlap a label for the two to count as one row.
	minLabelRowOverlap = 0.5
)

// sensitiveMarkers are the words that make a region sensitive on their own.
var sensitiveMarkers = []string{
	"credential_marker", "secret_marker", "token_marker", "password", "api_key", "private_key",
}

var (
	emailPattern       = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	schemePattern      = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+\-.]*://\S+`)
	ticketPattern      = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-[0-9]+\b`)
	unixPathPattern    = regexp.MustCompile(`(?i)(^|[^a-z0-9])/(users|home|var|etc|private|volumes|tmp)/\S+`)
	windowsPathPattern = regexp.MustCompile(`(?i)\b[a-z]:\\[^\s]+`)

	// trailingNotePattern matches the note a form prints after a label, as in
	// "Password (required)". The note is not the label's value.
	trailingNotePattern = regexp.MustCompile(`\s*\([^()]*\)$`)
)

func containsSensitiveMarker(text string) bool {
	if containsMarkerWord(strings.ToLower(text)) {
		return true
	}
	return emailPattern.MatchString(text) ||
		schemePattern.MatchString(text) ||
		ticketPattern.MatchString(text) ||
		unixPathPattern.MatchString(text) ||
		windowsPathPattern.MatchString(text)
}

func containsMarkerWord(lower string) bool {
	for _, marker := range sensitiveMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func endsWithMarkerWord(lower string) bool {
	for _, marker := range sensitiveMarkers {
		if strings.HasSuffix(lower, marker) {
			return true
		}
	}
	return false
}

// isValueLabel reports whether a region names a secret and leaves its value
// for another region. Text ending in ":" or "=" after a marker word is a
// label, as is a short region that ends in a marker word, such as the
// "Confirm password" a boxed form renders with no colon. A trailing
// parenthetical note is ignored. Only marker words qualify: an email or URL is
// a value, not a label for the next region. A region that carries other text
// after the marker, such as "password: hunter2", holds its own value and opens
// no window.
func isValueLabel(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if strings.HasSuffix(lower, ")") {
		lower = trailingNotePattern.ReplaceAllString(lower, "")
	}
	body := strings.TrimRightFunc(lower, func(r rune) bool { return !isLetterOrDigit(r) })
	if strings.ContainsAny(lower[len(body):], ":=") {
		return containsMarkerWord(body)
	}
	return len(strings.Fields(body)) <= maxBareLabelWords && endsWithMarkerWord(body)
}

func isLetterOrDigit(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// labelValueRegions marks the regions that hold a label's value, in engine
// order, so Extract can withhold them even though their own text matches no
// marker. A region is a value when it is one of the labelValueWindow non-blank
// regions after a label, or when it sits to the right of a label on the same
// row. The row clause exists because tesseract reads columns as separate
// blocks, which can put a value in the next column many regions after its
// label. Blank regions never become sections, so they do not use up the window.
// A region with no letter or digit does not use it up either, because box edges
// reach OCR as "|" or "_". That region is still withheld, since a password can
// be all symbols.
//
// The function reads engine order and never sorts: on a multi-column page,
// layout order is more accurate than a sort by position.
func labelValueRegions(regions []Region) []bool {
	values := make([]bool, len(regions))
	labels := []Bounds{}
	remaining := 0
	for i, region := range regions {
		text := strings.TrimSpace(region.Text)
		if text == "" {
			continue
		}
		if remaining > 0 {
			values[i] = true
			if strings.ContainsFunc(text, isLetterOrDigit) {
				remaining--
			}
		}
		if isValueLabel(text) {
			remaining = labelValueWindow
			labels = append(labels, region.Bounds)
		}
	}
	for i, region := range regions {
		values[i] = values[i] || slices.ContainsFunc(labels, func(label Bounds) bool {
			return rightOfOnSameRow(region.Bounds, label)
		})
	}
	return values
}

// rightOfOnSameRow reports whether region starts past label's right edge and
// overlaps its row. An engine that reports no height has no row, so the
// reading-order window is all that applies to it.
func rightOfOnSameRow(region Bounds, label Bounds) bool {
	if region.Height <= 0 || label.Height <= 0 || region.X < label.X+label.Width {
		return false
	}
	overlap := min(region.Y+region.Height, label.Y+label.Height) - max(region.Y, label.Y)
	return overlap >= minLabelRowOverlap*min(region.Height, label.Height)
}

func safeIdentity(value string, prefix string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if containsSensitiveMarker(value) || isUnsafeSourcePath(value) {
		return prefix + ":" + hashText(value), true
	}
	return value, false
}

func safeCanonicalURI(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if isSafeRelativeSource(value) {
		return value, false
	}
	return "", true
}

func safeSourceURI(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if isSafeRelativeSource(value) {
		return path.Clean(filepath.ToSlash(value))
	}
	return "redacted:" + hashText(value)
}

func isSafeRelativeSource(value string) bool {
	raw := strings.TrimSpace(value)
	if strings.Contains(raw, "://") || schemePattern.MatchString(raw) {
		return false
	}
	cleaned := path.Clean(filepath.ToSlash(raw))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
		return false
	}
	if containsSensitiveMarker(cleaned) {
		return false
	}
	return true
}

func isUnsafeSourcePath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.Contains(value, "://") {
		return true
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~/") {
		return true
	}
	if len(value) > 2 && value[1] == ':' && (value[2] == '\\' || value[2] == '/') {
		return true
	}
	return false
}
