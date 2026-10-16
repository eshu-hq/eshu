// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package composer

import "regexp"

// FixtureDirPattern matches a repository-relative slash path that has a
// directory segment named test, tests, fixtures, testdata, or examples, in any
// ASCII letter case. Only whole segments match, so packages/testing and
// packages/attestation do not. A composer.json under such a directory is test
// scaffolding or sample code, not a published package (#8223).
//
// The case folding is spelled out as ASCII bracket classes on purpose. Go's
// (?i) folds the long s (U+017F) to s and PostgreSQL's ~* does not, and ~* also
// follows the database locale, so a flag-based pattern would let the SQL anchor
// and the parser stamp disagree. This form uses no flag, no folding and no
// locale in either dialect, in the subset POSIX ERE and RE2 share. The producer
// anchor in internal/storage/postgres/code/producers embeds this constant in
// its SQL with !~ and InFixtureDir applies it in the parser: one source.
const FixtureDirPattern = `(^|/)([Tt][Ee][Ss][Tt][Ss]?|[Ff][Ii][Xx][Tt][Uu][Rr][Ee][Ss]|[Tt][Ee][Ss][Tt][Dd][Aa][Tt][Aa]|[Ee][Xx][Aa][Mm][Pp][Ll][Ee][Ss])/`

var fixtureDirRegexp = regexp.MustCompile(FixtureDirPattern)

// InFixtureDir reports whether the repository-relative slash path (a manifest
// path, or any path below the repository root) sits under a directory matching
// FixtureDirPattern. The final element is a file name and never counts.
func InFixtureDir(relativePath string) bool {
	return fixtureDirRegexp.MatchString(relativePath)
}
