// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"go/build"
)

// toolchainBuildContext is the build context the static scans honor:
// a .go file contributes symbols or evidence only when it builds
// under this context, so anchors and consumption that exist only
// behind an unsatisfied //go:build constraint never satisfy binding
// validation.
var toolchainBuildContext = build.Default

// fileBuilds reports whether the named .go file in dirAbs participates
// in the toolchain build. Files whose constraints cannot be evaluated
// fail closed: they contribute no symbols or evidence.
func fileBuilds(dirAbs, name string) bool {
	ok, err := toolchainBuildContext.MatchFile(dirAbs, name)
	return err == nil && ok
}
