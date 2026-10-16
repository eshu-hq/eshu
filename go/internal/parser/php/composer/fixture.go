// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package composer

import "regexp"

// FixtureDirPattern matches a repository-relative slash path that has a
// directory segment named tests, fixtures, testdata, or examples, in any
// letter case. Only whole segments match, so packages/testing and
// packages/attestation do not. A composer.json under such a directory is test
// scaffolding or sample code, not a published package (#8223). The pattern is
// written in the subset POSIX ERE (PostgreSQL's ~*) and RE2 (Go) share, because
// the producer anchor in internal/storage/postgres/code/producers embeds this
// constant in its SQL and InFixtureDir applies it in the parser: one source,
// so the anchor and the package_id stamp cannot disagree.
const FixtureDirPattern = `(^|/)(tests|fixtures|testdata|examples)/`

var fixtureDirRegexp = regexp.MustCompile(`(?i)` + FixtureDirPattern)

// InFixtureDir reports whether the repository-relative slash path (a manifest
// path, or any path below the repository root) sits under a directory matching
// FixtureDirPattern. The final element is a file name and never counts.
func InFixtureDir(relativePath string) bool {
	return fixtureDirRegexp.MatchString(relativePath)
}
