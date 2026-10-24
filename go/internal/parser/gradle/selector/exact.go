// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package selector

// latestPrefix starts Gradle's status selectors: latest.release,
// latest.integration, latest.milestone, and any custom status.
const latestPrefix = "latest."

// Exact reports whether text names one exact Gradle version, the only text a
// Gradle reader may mark resolved or hand on as an observed version.
//
// It fails closed. Text is exact only when it is non-empty printable ASCII
// with none of the characters that start whitespace, a quote, an escape, an
// interpolation, or a shell construct, and when Gradle would not read it as a
// selector or a modifier. Each of these is rejected:
//
//   - '+' anywhere: the prefix selector ("1.+", "1.0.+", "+"). Gradle reads
//     only a trailing '+' as a prefix; the rule rejects every '+' so an
//     interior one ("1.0.0+build.1") never resolves either.
//   - '[', ']', '(', ')' or ',': a Maven-style range ("[1.0,2.0)",
//     "]1.0,2.0[", "(,2.0]", "[1.0]").
//   - '!': the strict short-hand ("1.0!!", "[1.0,2.0)!!1.5"). Gradle reads
//     "1.0!!" as strictly 1.0, so even that text is not the version.
//   - '@': the artifact-only notation ("1.0@jar").
//   - ':': the string-notation separator. A version never holds one, so a
//     ':' means a classifier or other notation text leaked into it.
//   - a "latest." prefix in any letter case: a status selector.
//
// Exact applies no length cap; a caller with a field bound checks it first.
func Exact(text string) bool {
	if text == "" {
		return false
	}
	for i := 0; i < len(text); i++ {
		if !exactByte(text[i]) {
			return false
		}
	}
	return !hasLatestPrefix(text)
}

// exactByte reports whether b may appear in an exact version.
func exactByte(b byte) bool {
	if b <= ' ' || b >= 0x7f {
		return false
	}
	switch b {
	case '"', '\'', '\\', '$', '`', '{', '}', '<', '>', '|', ';', '&',
		'+', '!', '@', '[', ']', '(', ')', ',', ':':
		return false
	}
	return true
}

// hasLatestPrefix reports whether text starts with "latest." ignoring ASCII
// letter case. Exact has already proven text is ASCII.
func hasLatestPrefix(text string) bool {
	if len(text) < len(latestPrefix) {
		return false
	}
	for i := 0; i < len(latestPrefix); i++ {
		b := text[i]
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if b != latestPrefix[i] {
			return false
		}
	}
	return true
}
