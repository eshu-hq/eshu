// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package npmrc

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/secret/detect"
)

const (
	// tokenRunBytes is the length of one separator-free alphanumeric run that
	// holds both a letter and a digit and is treated as a token. Registry path
	// words (npm, virtual, packaging, a numeric project id) stay far below it.
	tokenRunBytes = 20
	// longWordBytes is the length at which a single letters-only run is treated
	// as a token.
	longWordBytes = 32
	// mixedSegmentBytes is the length at which a segment that mixes upper case,
	// lower case, and a digit is treated as a token even when '-' and '_' split
	// it into short runs (base64url tokens).
	mixedSegmentBytes = 20
)

var uuidPattern = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// pathMayHoldCredential reports whether a URL path might carry a credential.
// The shared detector sees keyed and well-known value shapes only, so a token
// placed as a bare path segment needs a structural check as well. The check is
// fail-closed on shape: a false positive withholds an ordinary path and costs
// the path field only, never the scheme or host.
func pathMayHoldCredential(escapedPath string) bool {
	decoded, err := url.PathUnescape(escapedPath)
	if err != nil || strings.Contains(decoded, "%") {
		// An undecodable or double-encoded path hides what it holds.
		return true
	}
	if len(detect.Spans(decoded)) > 0 {
		return true
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segmentMayHoldCredential(segment) {
			return true
		}
	}
	return false
}

func segmentMayHoldCredential(segment string) bool {
	if segment == "" {
		return false
	}
	if strings.ContainsAny(segment, "=:") || uuidPattern.MatchString(segment) {
		return true
	}
	var upper, lower, digit bool
	run, runLetter, runDigit := 0, false, false
	endRun := func() bool {
		long := run >= tokenRunBytes && runLetter && runDigit || run >= longWordBytes
		run, runLetter, runDigit = 0, false, false
		return long
	}
	for i := 0; i < len(segment); i++ {
		c := segment[i]
		switch {
		case c >= 'A' && c <= 'Z':
			upper, runLetter = true, true
		case c >= 'a' && c <= 'z':
			lower, runLetter = true, true
		case c >= '0' && c <= '9':
			digit, runDigit = true, true
		default:
			if endRun() {
				return true
			}
			continue
		}
		run++
	}
	if endRun() {
		return true
	}
	return len(segment) >= mixedSegmentBytes && upper && lower && digit
}
