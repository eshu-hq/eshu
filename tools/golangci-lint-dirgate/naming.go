// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "strings"

// namingViolation is one file whose name collides with a sibling
// subpackage's name and should therefore live inside that subpackage.
type namingViolation struct {
	// File is the qualifying file's basename, e.g. "bar_baz.go".
	File string
	// Subpackage is the sibling subdirectory (and, by the directory-name
	// convention this gate assumes, package) the file belongs in.
	Subpackage string
}

// detectNamingViolations reports every file in files whose name signals it
// belongs inside one of subpkgs instead of the current directory.
//
// The exact rule (issue #6054): let stem be a qualifying file's basename
// with the trailing ".go" removed. stem violates against a subpackage name
// sub when:
//
//	stem == sub                     (e.g. "bar.go" next to a "bar/" package)
//	stem starts with sub + "_"       (e.g. "bar_baz.go" next to "bar/")
//
// Both conditions require an exact word boundary: the prefix match is
// anchored on the underscore, not on the raw substring. That is
// deliberate -- it is what keeps "awscloud_scanner.go" from tripping
// against a "aws/" subpackage, and "barnacle.go" from tripping against a
// "bar/" subpackage. A same-prefix, no-boundary name is not a naming
// violation under this rule.
//
// files is expected to already be the qualifying (non-test) set used for
// the size cap; see evaluateDirectory.
func detectNamingViolations(files, subpkgs []string) []namingViolation {
	if len(subpkgs) == 0 {
		return nil
	}
	var out []namingViolation
	for _, f := range files {
		stem := strings.TrimSuffix(f, ".go")
		for _, sub := range subpkgs {
			if stem == sub || strings.HasPrefix(stem, sub+"_") {
				out = append(out, namingViolation{File: f, Subpackage: sub})
				break
			}
		}
	}
	return out
}
