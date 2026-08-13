// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bufio"
	"os"
	"strings"
)

// nolintJustification scans path for a `//nolint:<gate>` marker on its
// `package` declaration line and returns the written justification that
// must follow it, mirroring the filelength plugin's escape-hatch
// convention (tools/golangci-lint-filelength/README.md "Exempting a
// file") but -- unlike that convention -- actually enforcing the
// justification requirement rather than relying on reviewer discipline
// (issue #6054: "a bare suppression with no justification must NOT be
// accepted").
//
// The accepted shape is exactly:
//
//	package foo //nolint:<gate> // <non-empty justification text>
//
// A marker with no second "//", or with only whitespace after it, is
// treated as absent (ok is false): the caller must still enforce the
// finding. This function does not use golangci-lint's own nolint
// processor, because that processor suppresses on marker PRESENCE alone
// (any //nolint:<gate>, justified or not) and cannot express "justified
// markers only" -- the same reason the filelength gate's convention is
// enforced by review, not by tooling. The authoritative enforcement of
// this requirement is scripts/lib/dirgate-core.sh's bash mirror, which every
// local and CI path in specs/ci-gates.v1.yaml actually invokes; this
// function keeps the golangci-lint plugin path as close to that behavior
// as a single per-package Go analyzer pass can get.
func nolintJustification(path, gate string) (justification string, ok bool) {
	f, err := os.Open(path) // #nosec G304 -- path is a source file this package already discovered via os.ReadDir, not user input
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }() // read-only fd; a close failure here has nothing left to report to

	marker := "//nolint:" + gate
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(strings.TrimSpace(line), "package ") {
			continue
		}
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		after := line[idx+len(marker):]
		// The next non-space content must start a NEW "//" comment (the
		// justification), not just continue the marker (e.g.
		// "//nolint:dirgateway" must not match "//nolint:dirgate").
		if len(after) > 0 && !isCommentBoundary(after) {
			continue
		}
		reasonStart := strings.Index(after, "//")
		if reasonStart < 0 {
			return "", false
		}
		reason := strings.TrimSpace(after[reasonStart+2:])
		if reason == "" {
			return "", false
		}
		return reason, true
	}
	return "", false
}

// isCommentBoundary reports whether after (the text immediately following
// a matched "//nolint:<gate>" token) starts with either whitespace or a
// "//" -- i.e. the marker token actually ended there instead of
// continuing into a longer identifier.
func isCommentBoundary(after string) bool {
	r := after[0]
	return r == ' ' || r == '\t' || (len(after) >= 2 && after[:2] == "//")
}
