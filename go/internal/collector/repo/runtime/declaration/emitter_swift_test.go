// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import "testing"

// TestEmitSwiftVersion pins the .swift-version grammar (#8959 admitted the
// file; #8969 review). The file holds one toolchain on its first effective
// line. Only a plain numeric release ("5.9", "6.0.1") yields version parts;
// a release tag, a snapshot or a channel name stays an alias as written, so a
// development snapshot is never reported as the release it precedes.
func TestEmitSwiftVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, path, body string
		want             []wantFact
	}{
		{
			"partial release", ".swift-version", "5.9\n",
			[]wantFact{{".", "swift_version", "swift", "<nil>", "5.9", "partial", "<nil>", 1, 0, 5, 9, -1}},
		},
		{
			"exact release in a package", "Packages/Core/.swift-version", "6.0.1\n",
			[]wantFact{{"Packages/Core", "swift_version", "swift", "<nil>", "6.0.1", "exact", "<nil>", 1, 0, 6, 0, 1}},
		},
		{
			"major only", ".swift-version", "6\n",
			[]wantFact{{".", "swift_version", "swift", "<nil>", "6", "partial", "<nil>", 1, 0, 6, -1, -1}},
		},
		{
			"release tag is an alias", ".swift-version", "swift-5.9-RELEASE\n",
			[]wantFact{{".", "swift_version", "swift", "<nil>", "swift-5.9-RELEASE", "alias", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"main snapshot is an alias", ".swift-version", "main-snapshot-2024-01-01\n",
			[]wantFact{{".", "swift_version", "swift", "<nil>", "main-snapshot-2024-01-01", "alias", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"versioned development snapshot is an alias", ".swift-version", "5.10-DEVELOPMENT-SNAPSHOT-2024-01-01-a\n",
			[]wantFact{{".", "swift_version", "swift", "<nil>", "5.10-DEVELOPMENT-SNAPSHOT-2024-01-01-a", "alias", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"v prefix is not a swift spelling", ".swift-version", "v5.9\n",
			[]wantFact{{".", "swift_version", "swift", "<nil>", "v5.9", "alias", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"wildcard is not a swift spelling", ".swift-version", "5.x\n",
			[]wantFact{{".", "swift_version", "swift", "<nil>", "5.x", "alias", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"comparator is not a swift spelling", ".swift-version", ">=5.9\n",
			[]wantFact{{".", "swift_version", "swift", "<nil>", ">=5.9", "alias", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"placeholder is unresolved", ".swift-version", "${SWIFT_VERSION}\n",
			[]wantFact{{".", "swift_version", "swift", "<nil>", "${SWIFT_VERSION}", "unresolved", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"only the first effective line counts", ".swift-version", "# toolchain\n\n5.9.2\n6.0\n",
			[]wantFact{{".", "swift_version", "swift", "<nil>", "5.9.2", "exact", "<nil>", 3, 0, 5, 9, 2}},
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

func TestEmitSwiftVersionEmitsNothingWhenEmpty(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"", "\n", "# no toolchain pinned\n"} {
		if got := Emit(testContext(".swift-version"), "repo-1", ".swift-version", body); len(got) != 0 {
			t.Fatalf("Emit(.swift-version, %q) = %d envelopes, want none", body, len(got))
		}
	}
}
