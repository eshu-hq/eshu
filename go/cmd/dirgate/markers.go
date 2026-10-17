// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"regexp"
	"strings"
)

// nolintMarker is the per-file escape: `//nolint:dirgate // <reason>` on a
// file's package line. It is the only escape from the naming rule. It is not an
// escape from the directory cap.
const nolintMarker = "//nolint:dirgate"

// marker is one nolint:dirgate comment found on a package line.
type marker struct {
	path      string // repository-relative, e.g. go/internal/x/doc.go
	dir       string // go/-relative directory
	reason    string
	hasReason bool
}

var grepLine = regexp.MustCompile(`^[0-9a-f]+:(go/.+\.go):[0-9]+:(.*)$`)

// loadMarkers lists every nolint:dirgate package-line marker in tree.
func loadMarkers(g gitRepo, tree string) (map[string]marker, error) {
	// User grep.* and color settings change the output shape (grep.column adds a
	// column field, color.grep adds escape codes), so pin them for this call.
	out, err := g.output("-c", "grep.column=false", "-c", "grep.lineNumber=true", "-c", "color.grep=false", "-c", "color.ui=false",
		"grep", "--no-color", "-n", "-I", "-E", `^[[:space:]]*package .*//nolint:dirgate`, tree, "--", ":(top)"+goRoot)
	if err != nil && exitCode(err) != 1 { // exit 1 is "no match"
		return nil, fmt.Errorf("scan nolint markers in %s: %w", short(tree), err)
	}
	markers := map[string]marker{}
	for _, line := range strings.Split(out, "\n") {
		m := grepLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		reason, has, present := markerReason(m[2])
		if !present {
			continue
		}
		file := m[1]
		dir := strings.TrimPrefix(file[:strings.LastIndex(file, "/")], goRoot+"/")
		if !strings.Contains(file[len(goRoot)+1:], "/") {
			dir = "."
		}
		markers[file] = marker{path: file, dir: dir, reason: reason, hasReason: has}
	}
	return markers, nil
}

// markerReason reads a package line. present reports a real marker; hasReason
// reports a non-empty justification after a second `//`.
func markerReason(line string) (reason string, hasReason, present bool) {
	if !strings.HasPrefix(strings.TrimSpace(line), "package ") {
		return "", false, false
	}
	idx := strings.Index(line, nolintMarker)
	if idx < 0 {
		return "", false, false
	}
	after := line[idx+len(nolintMarker):]
	// "//nolint:dirgateway" is a different linter name, not this marker.
	if len(after) > 0 && after[0] != ' ' && after[0] != '\t' && !strings.HasPrefix(after, "//") {
		return "", false, false
	}
	start := strings.Index(after, "//")
	if start < 0 {
		return "", false, true
	}
	reason = strings.TrimSpace(after[start+2:])
	return reason, reason != "", true
}
