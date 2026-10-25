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
// slash or an at sign. So a row with a direct source or an unparsed line loses
// its value, source URL, and raw text and keeps only its kind. A registry pin
// is kept whole unless its specifier holds an at sign or a scheme separator,
// which no version specifier contains; that row is demoted to malformed with
// its name and text dropped.
func (b *rowBuilder) applyConstraintPolicy() {
	b.CentralVersionDeclaration = true
	if b.ConfigKind == configKindDependency &&
		(strings.Contains(b.Value, "@") || strings.Contains(b.Value, "://")) {
		b.Name = ""
		b.ConfigKind = configKindMalformed
		b.Malformed = true
	}
	if b.ConfigKind == configKindDependency {
		return
	}
	b.Value = ""
	b.SourceURL = ""
	b.SourceRef = ""
	b.Raw = ""
	b.Marker = ""
	if b.Malformed || strings.Contains(b.Name, "@") || strings.Contains(b.Name, "://") {
		b.Name = ""
	}
}
