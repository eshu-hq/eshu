// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// import_bindings.go pins the #10301 parser output for blank and dot import
// bindings: the blank ("_") and dot (".") bindings are recorded verbatim in
// the import's "alias" so delta CALLS derivation can tell a file whose bare
// names may come from a dot-imported package apart from one whose bare names
// are provably same-package. The bare ToUpper call below resolves through
// the dot-imported strings package, not the same package.
package main

import (
	"fmt"

	_ "embed"
	. "strings" //nolint:staticcheck // ST1001: fixture exists to pin dot-import alias output (#10301)
)

// RenderShouted upper-cases its input through the dot-imported strings
// package and prints it, pinning both the import-alias output and a bare
// call fact for this file.
func RenderShouted(input string) {
	fmt.Println(ToUpper(input))
}
