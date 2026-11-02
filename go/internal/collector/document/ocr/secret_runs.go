// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import (
	"strings"
	"unicode"
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

	// minTokensBeforeTail is how many run tokens in a row must come before the
	// short last line of a body for it to be withheld with them.
	minTokensBeforeTail = 2
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
//     text is a run token: at least minRunTokenLen characters, a digit or one of
//     `+/=`, and at most a stray space (see denseSpaceShare). Prose has a space
//     every five or six characters, so it ends a run at the first line of
//     ordinary text.
//   - the two regions before it were withheld run tokens, and its own text has
//     no more than a stray space. A body ends on a short line (`Qk9x==`), and this
//     keeps the tail of the key out of the facts. It holds for one region only,
//     and it needs two tokens before it, so one withheld value such as a
//     password does not hide the single word under it.
//   - it holds no letter or digit and the region before it was withheld. Box
//     edges reach OCR as "|", and the label window treats them the same way. Such a
//     region neither ends the run nor adds to it.
//   - its own text is a seed token (see isSeedToken), whatever came before. A
//     key body cropped from its armor line has no label to open a window. A
//     seed counts as two tokens, because it is long enough to stand alone.
//
// A label region needs no mark of its own: the two regions after it are in the
// window already, so the first region of a run is always a value. The scan reads
// engine order, makes one pass, and looks at each region's text a fixed number
// of times, so it is linear in the text, like the label scan. It changes
// values only, from false to true, so it never lets a region out that the
// window withheld.
func extendSecretRuns(regions []Region, values []bool) {
	previousWithheld := false
	tokens := 0 // withheld run tokens in a row, up to the region before this one
	for i, region := range regions {
		text := strings.TrimSpace(region.Text)
		if text == "" {
			continue
		}
		if !strings.ContainsFunc(text, isLetterOrDigit) {
			// Box edges reach OCR as "|" or "_". Like the window, the run
			// withholds such a region and neither ends nor extends it.
			values[i] = values[i] || previousWithheld
			continue
		}
		withheld := values[i]
		token := isRunToken(text)
		switch {
		case withheld:
		case previousWithheld && token:
			withheld = true
		case tokens >= minTokensBeforeTail && isDense(text, 1):
			withheld, token = true, false
		case isSeedToken(text):
			withheld = true
		}
		values[i] = values[i] || withheld
		previousWithheld = withheld
		switch {
		case withheld && isSeedToken(text):
			tokens = minTokensBeforeTail
		case withheld && token:
			tokens++
		default:
			tokens = 0
		}
	}
}

// denseSpaceShare is the share of spaces a dense line may hold: at most one
// space for every denseSpaceShare other characters. Tesseract puts a space inside
// a line of base64 now and then (the sandbox fixture reads `zwZWd fRLgXL+Z1bv...`
// with one), and a misread space must not split the line from its key. A line of
// prose holds a space for every five or six characters, far over this share.
const denseSpaceShare = 16

// denseLen returns the number of characters in text that are not whitespace, and
// whether text holds few enough spaces to count as one line of a token: at most
// one for every denseSpaceShare other characters.
func denseLen(text string) (int, bool) {
	spaces, others := 0, 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			others++
		}
	}
	return others, spaces*denseSpaceShare <= others
}

// isDense reports whether text, trimmed, is a dense line of at least minLen
// characters besides whitespace.
func isDense(text string, minLen int) bool {
	others, ok := denseLen(text)
	return ok && others >= minLen
}

// isRunToken reports whether text, trimmed, is one dense line long enough to be a
// line of a wrapped secret: minRunTokenLen characters apart from a stray space,
// and a digit or one of `+/=`. The charset is open on purpose, because OCR reads
// `l` as `|` and a line with one misread character is still a line of the key.
func isRunToken(text string) bool {
	if !isDense(text, minRunTokenLen) {
		return false
	}
	return strings.ContainsFunc(text, func(r rune) bool {
		return unicode.IsDigit(r) || r == '+' || r == '/' || r == '='
	})
}

// isSeedToken reports whether text, trimmed, looks like a base64 or token line
// on its own: minSeedTokenLen characters of letters, digits, and `+/=_.-`, apart
// from a stray space, with a capital letter, a lowercase letter, and a digit.
// Random base64 of that length holds all three with near certainty. A path, a
// hash, an environment variable name, and a sentence each lack one of them or a
// character of the set.
func isSeedToken(text string) bool {
	if !isDense(text, minSeedTokenLen) {
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
		case c == '+' || c == '/' || c == '=' || c == '_' || c == '.' || c == '-' || c == ' ':
		default:
			return false
		}
	}
	return upper && lower && digit
}
