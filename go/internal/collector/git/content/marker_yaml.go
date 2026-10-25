// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"errors"
	"regexp"
	"strings"
)

// errYAMLIndirection marks a body whose credential value marker mode cannot
// reach on the key's own line: a listed key whose value is a YAML alias leaves
// the secret on the anchor's line, and an explicit "? key" entry puts the value
// on a line that holds no key. The caller stores a metadata row instead (#8833
// security review).
var errYAMLIndirection = errors.New("content body reaches a credential key through a YAML alias or an explicit key")

// checkYAMLIndirection returns errYAMLIndirection when body holds:
//
//   - an explicit-key indicator "?" outside a quoted scalar, at the start of a
//     line, after a "- " list marker, or after "{", "[", or "," in a flow
//     collection, followed by white space or the end of the line;
//   - a line whose content starts with ":" and white space, the value line of
//     an explicit key;
//   - a line where a key one of keyPatterns matches has an alias (*name) as its
//     value. Pass the credential-key and the URL-userinfo-key patterns.
//
// An alias on any other key, an anchor on a listed key's value, and a merge key
// are left alone: the line that holds the secret is still a listed key's line.
func checkYAMLIndirection(body string, keyPatterns ...*regexp.Regexp) error {
	for _, line := range strings.Split(body, "\n") {
		if explicitKeyLine(line) {
			return errYAMLIndirection
		}
		for _, pattern := range keyPatterns {
			if pattern == nil {
				continue
			}
			for _, loc := range pattern.FindAllStringIndex(line, -1) {
				if strings.HasPrefix(strings.TrimLeft(line[loc[1]:], " \t"), "*") {
					return errYAMLIndirection
				}
			}
		}
	}
	return nil
}

// explicitKeyLine reports whether line holds an explicit-key indicator or is
// the value line of an explicit key.
func explicitKeyLine(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	for strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "-\t") {
		trimmed = strings.TrimLeft(trimmed[2:], " \t")
	}
	if indicatorAt(trimmed, 0, ':') {
		return true
	}
	quote := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return false
		case c == '?' && indicatorAt(line, i, '?') && opensEntry(line[:i]):
			return true
		}
	}
	return false
}

// indicatorAt reports whether s[i] is the indicator byte followed by white
// space or the end of s.
func indicatorAt(s string, i int, indicator byte) bool {
	return i < len(s) && s[i] == indicator && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t')
}

// opensEntry reports whether the text before an indicator ends where a new
// mapping entry can start: nothing, a list marker, or a flow "{", "[", or ",".
func opensEntry(before string) bool {
	before = strings.TrimRight(before, " \t")
	if before == "" {
		return true
	}
	switch before[len(before)-1] {
	case '-', '{', '[', ',':
		return true
	}
	return false
}
