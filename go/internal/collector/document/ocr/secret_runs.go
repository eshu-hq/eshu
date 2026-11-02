// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// minRunTokenLen is the shortest whitespace-free line that continues a run
	// of withheld regions. A PEM body line is 64 or 70 characters and a wrapped
	// token line is usually 30 or more, so 16 keeps well under a real line and
	// above the ordinary one-word lines of a document.
	minRunTokenLen = 16

	// minSeedTokenLen is the shortest line that starts a run on its own, with
	// no label before it. It is long on purpose: a seed has nothing else to
	// vouch for it.
	minSeedTokenLen = 40
)

// extendSecretRuns marks the regions of a secret that runs past the label
// window (#9799): the body of a PEM key and a token wrapped across lines. The
// window in labelValueRegions covers a value one or two lines long. A key body
// is 12 to 50 lines, so a region that follows a withheld region and is shaped
// like a token joins it, and so does the one after that.
//
// A region joins the run when it was not withheld already and any of these holds:
//
//   - the region before it (the nearest one with text) was withheld, and its own
//     text is a run token: no whitespace, at least minRunTokenLen characters,
//     and a digit or one of `+/=`. Prose has spaces, so it ends a run at the
//     first line of ordinary text.
//   - the region before it was a withheld run token, and its own text has no
//     whitespace at all. A body ends on a short line (`Qk9x==`), and this keeps
//     the tail of the key out of the facts. It holds for one region only.
//   - its own text is a seed token (see isSeedToken), whatever came before. A
//     key body cropped from its armor line has no label to open a window.
//
// A label region needs no mark of its own: the two regions after it are in the
// window already, so the first region of a run is always a value. The scan reads
// engine order, makes one pass, and looks at each region's text a fixed number
// of times, so it is linear in the text, like the label scan. It changes
// values only, from false to true, so it never lets a region out that the
// window withheld.
func extendSecretRuns(regions []Region, values []bool) {
	previousWithheld, previousToken := false, false
	for i, region := range regions {
		text := strings.TrimSpace(region.Text)
		if text == "" {
			continue
		}
		withheld := values[i]
		token := isRunToken(text)
		switch {
		case withheld:
		case previousWithheld && token:
			withheld = true
		case previousToken && !hasSpace(text):
			withheld = true
		case isSeedToken(text):
			withheld = true
		}
		values[i] = values[i] || withheld
		previousWithheld, previousToken = withheld, withheld && token
	}
}

// hasSpace reports whether text holds any whitespace.
func hasSpace(text string) bool {
	return strings.ContainsFunc(text, unicode.IsSpace)
}

// isRunToken reports whether text, trimmed, is one unbroken token long enough to
// be a line of a wrapped secret: no whitespace, minRunTokenLen characters, and a
// digit or one of `+/=`. The charset is open on purpose, because OCR reads `l` as
// `|` and a line with one misread character is still a line of the key.
func isRunToken(text string) bool {
	if utf8.RuneCountInString(text) < minRunTokenLen || hasSpace(text) {
		return false
	}
	return strings.ContainsFunc(text, func(r rune) bool {
		return unicode.IsDigit(r) || r == '+' || r == '/' || r == '='
	})
}

// isSeedToken reports whether text, trimmed, looks like a base64 or token line
// on its own: minSeedTokenLen characters of letters, digits, and `+/=_.-`, with a
// capital letter, a lowercase letter, and a digit. Random base64 of that length
// holds all three with near certainty. A path, a hash, an environment variable
// name, and a sentence each lack one of them or a character of the set.
func isSeedToken(text string) bool {
	if len(text) < minSeedTokenLen {
		return false
	}
	var upper, lower, digit bool
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c >= 'A' && c <= 'Z':
			upper = true
		case c >= 'a' && c <= 'z':
			lower = true
		case c >= '0' && c <= '9':
			digit = true
		case c == '+' || c == '/' || c == '=' || c == '_' || c == '.' || c == '-':
		default:
			return false
		}
	}
	return upper && lower && digit
}
