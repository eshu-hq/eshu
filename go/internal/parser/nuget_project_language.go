// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser //nolint:dirgate // Engine dispatch glue: parseNuGetProject must stay in package parser, and moving it into nuget/ would make that package import the parent Engine contract. The naming-exempt ledger is a frozen #6054 seed that may only shrink, so a violation introduced after it must carry its own justified marker.

import (
	nugetparser "github.com/eshu-hq/eshu/go/internal/parser/nuget"
)

// parseNuGetProject adapts the nuget package's MSBuild project-file parser
// into the engine dispatch contract. It stays a plain function rather than an
// *Engine method because .csproj parsing needs no engine state; the wrapper
// exists so the nuget package can keep the parent parser package out of its
// import graph.
func parseNuGetProject(path string, isDependency bool, options Options) (map[string]any, error) {
	return nugetparser.Parse(path, isDependency, sharedOptions(options))
}
