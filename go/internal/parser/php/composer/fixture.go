// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package composer

import "github.com/eshu-hq/eshu/go/internal/parser/shared"

// FixtureDirPattern is shared.FixtureDirPattern: whole directory segments named
// test, tests, fixtures, testdata, or examples, in any ASCII letter case. A
// composer.json under such a directory is test scaffolding or sample code, not a
// published package (#8223). The pattern now lives in the parser's shared
// package because the package.json producer applies the same rule (#8244); this
// alias keeps one source for the composer anchor and the PHP parser stamp.
const FixtureDirPattern = shared.FixtureDirPattern

// InFixtureDir reports whether the repository-relative slash path sits under a
// directory matching FixtureDirPattern. The final element is a file name and
// never counts.
func InFixtureDir(relativePath string) bool {
	return shared.InFixtureDir(relativePath)
}
