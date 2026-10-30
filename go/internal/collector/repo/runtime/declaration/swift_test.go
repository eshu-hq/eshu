// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"
)

// TestEmitSwiftToolsVersion pins the Package.swift swift-tools-version handoff
// (#9308 Others lane ii). The swift-tools-version comment declares the Swift
// toolchain through the distinct swift_tools_version source kind; the
// swift-version pin file keeps its own kind. Only the first marker counts.
func TestEmitSwiftToolsVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, path, body string
		want             []wantFact
	}{
		{
			"exact tools version", "Package.swift", "// swift-tools-version: 6.0.1\nimport PackageDescription\n",
			[]wantFact{{".", "swift_tools_version", "swift", "<nil>", "6.0.1", "exact", "<nil>", 1, 0, 6, 0, 1}},
		},
		{
			"partial tools version", "Package.swift", "// swift-tools-version: 5.9\n",
			[]wantFact{{".", "swift_tools_version", "swift", "<nil>", "5.9", "partial", "<nil>", 1, 0, 5, 9, -1}},
		},
		{
			"range tools version", "Package.swift", "// swift-tools-version: >=5.9\n",
			[]wantFact{{".", "swift_tools_version", "swift", "<nil>", ">=5.9", "range", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"snapshot name stays an alias", "Package.swift", "// swift-tools-version: main-snapshot-2024-01-01\n",
			[]wantFact{{".", "swift_tools_version", "swift", "<nil>", "main-snapshot-2024-01-01", "alias", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"placeholder is unresolved", "Package.swift", "// swift-tools-version: ${SWIFT_VERSION}\n",
			[]wantFact{{".", "swift_tools_version", "swift", "<nil>", "${SWIFT_VERSION}", "unresolved", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"marker line is the declaration line", "Packages/Core/Package.swift", "import PackageDescription\n\n// swift-tools-version: 5.9\n",
			[]wantFact{{"Packages/Core", "swift_tools_version", "swift", "<nil>", "5.9", "partial", "<nil>", 3, 0, 5, 9, -1}},
		},
		{
			"only the first marker counts", "Package.swift", "// swift-tools-version: 5.9\n// swift-tools-version: 6.0\n",
			[]wantFact{{".", "swift_tools_version", "swift", "<nil>", "5.9", "partial", "<nil>", 1, 0, 5, 9, -1}},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkFacts(t, emitDecoded(t, tc.path, tc.body), tc.want)
		})
	}
}

// TestEmitSwiftToolsVersionSkips pins the absent-versus-malformed split: a
// Package.swift without the marker is absent (no candidate, no skip), a
// marker without a value is malformed (manifest_unparseable), a
// credential-shaped value is dropped and counted (value_rejected), and any
// other .swift file never reads.
func TestEmitSwiftToolsVersionSkips(t *testing.T) {
	t.Parallel()

	if got := Emit(testContext("Package.swift"), "repo-1", "Package.swift", "import PackageDescription\n"); len(got) != 0 {
		t.Fatalf("Emit without a marker = %d envelopes, want none", len(got))
	}
	if _, skip := EmitFile(testContext("Package.swift"), "repo-1", "Package.swift", "import PackageDescription\n"); skip.Skipped() {
		t.Fatalf("absent marker skip = %+v, want none", skip)
	}

	_, skip := EmitFile(testContext("Package.swift"), "repo-1", "Package.swift", "// swift-tools-version:\n")
	if skip.Reason != SkipReasonManifestUnparseable {
		t.Fatalf("malformed marker skip = %+v, want %q", skip, SkipReasonManifestUnparseable)
	}

	credential := "// swift-tools-version: https://user:password@example.invalid/5.9\n"
	_, skip = EmitFile(testContext("Package.swift"), "repo-1", "Package.swift", credential)
	if skip.Reason != SkipReasonValueRejected || skip.Rejected != 1 {
		t.Fatalf("credential skip = %+v, want value_rejected with one rejection", skip)
	}

	if got := Emit(testContext("Sources/App.swift"), "repo-1", "Sources/App.swift", "// swift-tools-version: 5.9\n"); len(got) != 0 {
		t.Fatalf("Emit from a non-manifest .swift file = %d envelopes, want none", len(got))
	}
	if got := ClassifyPath("Sources/App.swift"); got != FileNotAdmitted {
		t.Fatalf("ClassifyPath(Sources/App.swift) = %v, want FileNotAdmitted", got)
	}
	if got := ClassifyPath("Package.swift"); got != FileManifest {
		t.Fatalf("ClassifyPath(Package.swift) = %v, want FileManifest", got)
	}

	over := "// swift-tools-version: 5.9\n" + strings.Repeat("// filler\n", MaxManifestBytesPerFile)
	_, skip = EmitFile(testContext("Package.swift"), "repo-1", "Package.swift", over)
	if skip.Reason != SkipReasonByteCap {
		t.Fatalf("over-cap skip = %+v, want %q", skip, SkipReasonByteCap)
	}
}
