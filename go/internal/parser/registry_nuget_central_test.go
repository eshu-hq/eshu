// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestDefaultRegistryBindsCentralNuGetPropsByExactName pins #8824: the two
// MSBuild shared-props files bind to the NuGet project parser by exact
// (case-insensitive) file name, wherever they sit in the repository.
func TestDefaultRegistryBindsCentralNuGetPropsByExactName(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	for _, name := range []string{
		"Directory.Packages.props",
		"Directory.Build.props",
		"directory.packages.props",
		"DIRECTORY.BUILD.PROPS",
	} {
		definition, ok := registry.LookupByPath(filepath.Join("repo", "src", name))
		if !ok {
			t.Fatalf("%s did not resolve to a parser", name)
		}
		if definition.ParserKey != "nuget_project" {
			t.Fatalf("%s = %q, want nuget_project", name, definition.ParserKey)
		}
	}
}

// TestDefaultRegistryKeepsNearMissPropsNamesUnbound is the negative side: only
// the two exact names bind. There is no generic .props rule, so a sibling props
// file, a backup copy, or a misspelling must stay unadmitted.
func TestDefaultRegistryKeepsNearMissPropsNamesUnbound(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	for _, name := range []string{
		"Directory.Packages.props.bak",
		"directory.packages.prop",
		"Packages.props",
		"Build.props",
		"Directory.Build.targets",
		"Directory.Packages.targets",
		"Common.props",
		"MyDirectory.Build.props",
		"Directory.Build.props.user",
	} {
		if definition, ok := registry.LookupByPath(filepath.Join("repo", name)); ok {
			t.Fatalf("%s resolved to %q, want no parser", name, definition.ParserKey)
		}
	}
	if definition, ok := registry.LookupByExtension(".props"); ok {
		t.Fatalf(".props extension resolved to %q, want no generic rule", definition.ParserKey)
	}
}
