// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/urlredact"
)

// TestRedactFreeTextBoundaryCorpus drives this package's free-text walk through
// the shared corpus in internal/urlredact. cmd/eshu drives its redactEndpoint
// through the identical rows.
//
// One table, two walks, on purpose. The previous fixture varied only the
// parameter name and never the separator, so "&" was the only boundary any test
// exercised and the two walks drifted on ";" and on a nested "?" without a
// single test going red. A row either walk cannot handle is recorded in the row
// with its reason, and urlredact's own check fails if that reason stops being
// true — a stale exemption is a red test, not a silently widened corpus.
func TestRedactFreeTextBoundaryCorpus(t *testing.T) {
	t.Parallel()

	for _, tc := range urlredact.BoundaryCases() {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			got, _ := redactFreeText(tc.Input)
			if got != tc.WantFreeText {
				t.Fatalf("redactFreeText(%q)\n got %q\nwant %q", tc.Input, got, tc.WantFreeText)
			}
			if err := tc.CheckFreeTextSecret(got); err != nil {
				t.Error(err)
			}

			// Capture runs Validate over its own output, so a second pass has
			// to be a no-op or a redacted bundle fails its own gate.
			again, _ := redactFreeText(got)
			if again != got {
				t.Fatalf("redactFreeText is not idempotent: second pass on %q gave %q", got, again)
			}
		})
	}
}
