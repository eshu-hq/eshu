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
	// labelValueWindow is how many regions with a letter or digit after a
	// marker region count as its value. One covers a value directly below its
	// label. The second covers a caption or other line between the two. Every
	// extra region hides one more line of ordinary text from a document, so the
	// window stays small and the row clause covers columns instead.
	labelValueWindow = 2

	// minLabelRowOverlap is the share of the shorter region's height that must
	// overlap a marker region for the two to count as one row.
	minLabelRowOverlap = 0.5

	// minColumnOverlap is the share of the shorter region's width that must
	// overlap a table header for a cell below it to count as in that
	// column. It mirrors minLabelRowOverlap on the horizontal axis, and an
	// exact half counts, as it does for the row clause.
	minColumnOverlap = 0.5
)

// identityMarkers are the words that make a source name sensitive. They are the
// narrow list on purpose: a file called token-rotation.png is a name, not text
// read from a picture, and the wider label list in label_markers.go would hide
// the identity of ordinary documents. Text read from a picture uses
// containsMarkerWord instead.
var identityMarkers = []string{
	"credential_marker", "secret_marker", "token_marker", "password", "api_key", "private_key",
}

var (
	emailPattern       = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	schemePattern      = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+\-.]*://\S+`)
	ticketPattern      = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-[0-9]+\b`)
	unixPathPattern    = regexp.MustCompile(`(?i)(^|[^a-z0-9])/(users|home|var|etc|private|volumes|tmp)/\S+`)
	windowsPathPattern = regexp.MustCompile(`(?i)\b[a-z]:\\[^\s]+`)
)

// containsSensitiveMarker reports whether a source name or path is sensitive.
func containsSensitiveMarker(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range identityMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return matchesSensitivePattern(text)
}

// containsSensitiveText reports whether one region of text read from a picture
// is sensitive on its own: a label from containsMarkerWord, or a value shaped
// like personal or private data.
func containsSensitiveText(text string) bool {
	return containsMarkerWord(text) || matchesSensitivePattern(text)
}

func matchesSensitivePattern(text string) bool {
	return emailPattern.MatchString(text) ||
		schemePattern.MatchString(text) ||
		ticketPattern.MatchString(text) ||
		unixPathPattern.MatchString(text) ||
		windowsPathPattern.MatchString(text)
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
// stacked labels each cover their own value. The row clause exists because
// tesseract reads columns as separate blocks, which can put a value in the next
// column many regions after its label.
//
// A region with no letter or digit does not use up the window, because box
// edges reach OCR as "|" or "_". That region is still withheld, since a password
// can be all symbols. A blank region never becomes a section, so its mark is
// unused.
//
// A value that runs past the window, such as the body of a PEM key or a token
// wrapped across lines, is extended by extendSecretRuns in secret_runs.go. A
// cell that sits below a table header is withheld by extendTableColumns, which
// runs last so a column cell never seeds or extends a secret run.
//
// The function reads engine order and never sorts: on a multi-column page,
// layout order is more accurate than a sort by position.
func labelValueRegions(regions []Region) []bool {
	values := make([]bool, len(regions))
	labels := []Bounds{}
	markers := []int{}
	remaining := 0
	for i, region := range regions {
		text := strings.TrimSpace(region.Text)
		if remaining > 0 {
			values[i] = true
			if strings.ContainsFunc(text, isLetterOrDigit) {
				remaining--
			}
		}
		if containsMarkerWord(text) {
			remaining += labelValueWindow
			labels = append(labels, region.Bounds)
			markers = append(markers, i)
		}
	}
	for i, region := range regions {
		values[i] = values[i] || slices.ContainsFunc(labels, func(label Bounds) bool {
			return rightOfOnSameRow(region.Bounds, label)
		})
	}
	extendSecretRuns(regions, values)
	extendTableColumns(regions, values, markers)
	return values
}

// rightOfOnSameRow reports whether region starts past label's right edge and
// overlaps its row. Bounds that are not usable have no row, so only the
// reading-order window applies to them.
func rightOfOnSameRow(region Bounds, label Bounds) bool {
	if !usableBounds(region) || !usableBounds(label) || region.X < label.X+label.Width {
		return false
	}
	return overlapsRow(region, label)
}

// overlapsRow reports whether two regions share a row: their vertical overlap
// is at least half the shorter height. It is the row test the column rule
// reuses to tell a table header cell, which shares its row, from a prose
// label on its own line.
func overlapsRow(first Bounds, second Bounds) bool {
	overlap := min(first.Y+first.Height, second.Y+second.Height) - max(first.Y, second.Y)
	return overlap >= minLabelRowOverlap*min(first.Height, second.Height)
}

// extendTableColumns withholds the cells below a table header, such as the
// Password column under a "Service Password Owner" header row that tesseract
// reads as one cell per column. A header is a marker region with a same-row
// neighbor to its left: a label that opens its row, with its value to the
// right as forms lay out, keeps the pinned two-region window below it and
// opens no column, so single-column prose and form layouts pay nothing. A
// marker in the first column, and a header row tesseract reads as one wide
// line, likewise open no column: those stay documented limits. A cell joins
// the column when its top edge is at or below the header's bottom edge and
// its horizontal overlap with the header is at least half the shorter width.
// The pass only turns kept regions to withheld, runs after extendSecretRuns,
// and looks at each region a fixed number of times, so secret-run chaining
// and cost are unchanged.
func extendTableColumns(regions []Region, values []bool, markers []int) {
	headers := []Bounds{}
	for _, i := range markers {
		if !usableBounds(regions[i].Bounds) || !hasSameRowNeighborToTheLeft(regions, i) {
			continue
		}
		headers = append(headers, regions[i].Bounds)
	}
	for i, region := range regions {
		values[i] = values[i] || slices.ContainsFunc(headers, func(header Bounds) bool {
			return belowInColumn(region.Bounds, header)
		})
	}
}

// hasSameRowNeighborToTheLeft reports whether another usable region shares
// region's row and starts left of it. A form label opens its row, with its
// value to the right; a table header past the first column does not. Regions
// with identical bounds never qualify: their left edges tie.
func hasSameRowNeighborToTheLeft(regions []Region, self int) bool {
	mine := regions[self].Bounds
	for j, sibling := range regions {
		if j == self || !usableBounds(sibling.Bounds) || sibling.Bounds.X >= mine.X {
			continue
		}
		if overlapsRow(mine, sibling.Bounds) {
			return true
		}
	}
	return false
}

// belowInColumn reports whether region sits in header's column: its top edge
// is at or below the header's bottom edge, with horizontal overlap of at
// least half the shorter width. A region on the header's own row belongs to
// the row clause, and a value above its header stays, as the README pins.
func belowInColumn(region Bounds, header Bounds) bool {
	if !usableBounds(region) || !usableBounds(header) {
		return false
	}
	if region.Y < header.Y+header.Height {
		return false
	}
	overlap := min(region.X+region.Width, header.X+header.Width) - max(region.X, header.X)
	return overlap >= minColumnOverlap*min(region.Width, header.Width)
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
