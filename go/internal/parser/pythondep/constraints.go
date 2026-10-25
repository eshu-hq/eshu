// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pythondep

import (
	"path/filepath"
	"strings"
)

// isConstraintsFile reports whether path names a pip constraints file:
// constraints.txt, constraints-*.txt, or constraints_*.txt. The match is on
// the base name, case-insensitive, and needs the .txt extension so a policy
// file such as constraints-gatekeeper.yaml is never read as pip input.
func isConstraintsFile(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	stem, ok := strings.CutSuffix(name, ".txt")
	if !ok {
		return false
	}
	return stem == "constraints" ||
		strings.HasPrefix(stem, "constraints-") ||
		strings.HasPrefix(stem, "constraints_")
}

// applyConstraintPolicy turns a parsed requirement line from a constraints
// file into a version policy row. pip applies a constraint only when something
// else installs the package, so the row never proves a declaration: it carries
// central_version_declaration, the flag every consumption reader already drops
// (the NuGet central PackageVersion contract, #8824).
//
// A constraints file is the one place Eshu cannot tell a harmless direct
// reference from a credential-bearing one, because a password may contain a
// slash or an at sign. So the row never stores the raw line, and carries
// source_cache_withheld so the content shaper drops the entity's file-range
// snippet, which is that same line. A row with a direct source or an unparsed
// line also loses its value, source URL, and marker, and any name holding an at
// sign or a scheme separator. A registry pin is kept unless its specifier holds
// an at sign or a scheme separator, which no version specifier contains; that
// row is demoted to malformed with its name dropped. A marker is kept only when
// every character is in the safeMarker allowlist; a pin whose marker fails that
// test is demoted to malformed too. Dropping only the marker would leave two
// lines that differ only in their markers with one content identity, and the
// content writer would keep one of them.
//
// The file body is a separate content fact and keeps the whole file until the
// content-store policy (#8812) covers this parser.
func (b *rowBuilder) applyConstraintPolicy() {
	b.CentralVersionDeclaration = true
	b.SourceCacheWithheld = true
	b.Raw = ""
	if b.ConfigKind == configKindDependency &&
		(strings.Contains(b.Value, "@") || strings.Contains(b.Value, "://")) {
		b.Name = ""
		b.ConfigKind = configKindMalformed
		b.Malformed = true
	}
	if b.ConfigKind == configKindDependency && !safeMarker(b.Marker) {
		b.Name = ""
		b.Value = ""
		b.ConfigKind = configKindMalformed
		b.Malformed = true
	}
	if b.ConfigKind == configKindDependency {
		return
	}
	b.Value = ""
	b.SourceURL = ""
	b.SourceRef = ""
	b.Marker = ""
	if b.Malformed || strings.Contains(b.Name, "@") || strings.Contains(b.Name, "://") {
		b.Name = ""
	}
}

// safeMarker reports whether a PEP 508 environment marker uses only letters,
// digits, space, tab, and the characters . _ - ~ ' " < > = ! ( ) , *. It is an
// allowlist, so any other character (an at sign, colon, slash, hash, plus,
// percent, semicolon, brace, or non-ASCII byte) fails: such text could be
// URL-shaped.
func safeMarker(marker string) bool {
	for i := 0; i < len(marker); i++ {
		ch := marker[i]
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		case strings.IndexByte(" \t._-<>=!~'\"(),*", ch) >= 0:
		default:
			return false
		}
	}
	return true
}
