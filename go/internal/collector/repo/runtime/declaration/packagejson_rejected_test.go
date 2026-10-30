// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import "testing"

// TestEmitPackageJSONCountsEveryStringThatYieldsNoDeclaration proves the rule
// the README states for the credential guard: a string value in a named
// section that does not become a declaration is dropped and counted, so an
// operator can see why a manifest has no fact. An empty or whitespace-only
// value, an empty spec after "@", and a packageManager that names no version
// are counted through Skip, not only left unemitted. A neighbour that is a
// valid declaration still emits.
func TestEmitPackageJSONCountsEveryStringThatYieldsNoDeclaration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		body         string
		wantEmitted  int
		wantRejected int
	}{
		{"empty engines value", `{"engines":{"node":""}}`, 0, 1},
		{"whitespace-only engines value", `{"engines":{"node":"   "}}`, 0, 1},
		{"empty volta value", `{"volta":{"node":""}}`, 0, 1},
		{"empty engines value beside a valid one", `{"engines":{"node":"","npm":"^10"}}`, 1, 1},
		{"packageManager with an empty spec", `{"packageManager":"pnpm@"}`, 0, 1},
		{"packageManager whose spec is only an integrity suffix", `{"packageManager":"pnpm@+sha512.abc"}`, 0, 1},
		{"packageManager that names no version", `{"packageManager":"pnpm"}`, 0, 1},
		{"empty packageManager", `{"packageManager":""}`, 0, 1},
		{"whitespace-only packageManager", `{"packageManager":"  "}`, 0, 1},
		{"unlisted packageManager name", `{"packageManager":"other@1.1.0"}`, 0, 1},
		{"bun without a version", `{"packageManager":"bun@"}`, 0, 1},
		{"deno URL is not a version", `{"packageManager":"deno@https://example.invalid/token"}`, 0, 1},
		{"valid packageManager is not counted", `{"packageManager":"pnpm@9.1.0"}`, 1, 0},
		{"non-string packageManager is absent, not rejected", `{"packageManager":42}`, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes, skip := EmitFile(testContext("package.json"), "repo-1", "package.json", tc.body)
			if len(envelopes) != tc.wantEmitted || skip.Rejected != tc.wantRejected {
				t.Fatalf("emitted %d, skip %+v, want %d emitted and %d rejected", len(envelopes), skip, tc.wantEmitted, tc.wantRejected)
			}
			if tc.wantRejected > 0 && skip.Reason != SkipReasonValueRejected {
				t.Fatalf("skip = %+v, want reason %q", skip, SkipReasonValueRejected)
			}
		})
	}
}
