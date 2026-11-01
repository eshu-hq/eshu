// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import (
	"math"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

const (
	// labelValueWindow is how many regions with a letter or digit after a marker
	// region count as its value. One covers a value directly below its label. The second
	// covers a caption or other line between the two. Every extra region hides
	// one more line of ordinary text from a document, so the window stays small
	// and the row clause covers columns instead.
	labelValueWindow = 2

	// minLabelRowOverlap is the share of the shorter region's height that must
	// overlap a marker region for the two to count as one row.
	minLabelRowOverlap = 0.5
)

// sensitiveMarkers are the words that make a region sensitive on their own.
// Unlike the patterns below, a marker word names a value rather than being one,
// so only a marker word opens a value window.
var sensitiveMarkers = []string{
	"credential_marker", "secret_marker", "token_marker", "password", "api_key", "private_key",
}

var (
	emailPattern       = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	schemePattern      = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+\-.]*://\S+`)
	ticketPattern      = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-[0-9]+\b`)
	unixPathPattern    = regexp.MustCompile(`(?i)(^|[^a-z0-9])/(users|home|var|etc|private|volumes|tmp)/\S+`)
	windowsPathPattern = regexp.MustCompile(`(?i)\b[a-z]:\\[^\s]+`)
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

func isLetterOrDigit(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// labelValueRegions marks the regions that hold a label's value, in engine
// order, so Extract can withhold them even though their own text matches no
// marker. Any region with a marker word opens a window, not only one shaped
// like a label: the words around a marker are the least reliable part of OCR
// text, and a leaked secret costs more than a hidden line. A region is a value
// when it falls in the window of labelValueWindow regions after a marker
// region, or when it sits to the right of a marker region on the same row. Each
// marker region adds a full window once, however many marker words it holds, so
// stacked labels each cover their own value. The row clause exists because tesseract reads columns as separate
// blocks, which can put a value in the next column many regions after its
// label.
//
// A region with no letter or digit does not use up the window, because box
// edges reach OCR as "|" or "_". That region is still withheld, since a password
// can be all symbols. A blank region never becomes a section, so its mark is
// unused.
//
// The function reads engine order and never sorts: on a multi-column page,
// layout order is more accurate than a sort by position.
func labelValueRegions(regions []Region) []bool {
	values := make([]bool, len(regions))
	labels := []Bounds{}
	remaining := 0
	for i, region := range regions {
		text := strings.TrimSpace(region.Text)
		if remaining > 0 {
			values[i] = true
			if strings.ContainsFunc(text, isLetterOrDigit) {
				remaining--
			}
		}
		if containsMarkerWord(strings.ToLower(text)) {
			remaining += labelValueWindow
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
// overlaps its row. Bounds that are not usable have no row, so only the
// reading-order window applies to them.
func rightOfOnSameRow(region Bounds, label Bounds) bool {
	if !usableBounds(region) || !usableBounds(label) || region.X < label.X+label.Width {
		return false
	}
	overlap := min(region.Y+region.Height, label.Y+label.Height) - max(region.Y, label.Y)
	return overlap >= minLabelRowOverlap*min(region.Height, label.Height)
}

// usableBounds rejects a region with no height or width and any NaN or infinite
// value, which an engine should never report and which compare unpredictably.
func usableBounds(bounds Bounds) bool {
	sum := bounds.X + bounds.Y + bounds.Width + bounds.Height
	return bounds.Width > 0 && bounds.Height > 0 && !math.IsNaN(sum) && !math.IsInf(sum, 0)
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
