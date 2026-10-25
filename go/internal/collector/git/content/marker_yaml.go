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
// on the next line. The caller stores a metadata row instead (#8833 security
// review).
var errYAMLIndirection = errors.New("content body reaches a credential key through a YAML alias or an explicit key")

// checkYAMLIndirection returns errYAMLIndirection when body holds an explicit
// "? key" entry anywhere, or a line where a key keyPattern matches has an alias
// (*name) as its value. A YAML alias on any other key, an anchor on a
// credential value, and a merge key are left alone: the line that holds the
// secret is still a listed key's line.
func checkYAMLIndirection(body string, keyPattern *regexp.Regexp) error {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		for strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "-\t") {
			trimmed = strings.TrimLeft(trimmed[2:], " \t")
		}
		if trimmed == "?" || strings.HasPrefix(trimmed, "? ") || strings.HasPrefix(trimmed, "?\t") {
			return errYAMLIndirection
		}
		for _, loc := range keyPattern.FindAllStringIndex(line, -1) {
			if strings.HasPrefix(strings.TrimLeft(line[loc[1]:], " \t"), "*") {
				return errYAMLIndirection
			}
		}
	}
	return nil
}
