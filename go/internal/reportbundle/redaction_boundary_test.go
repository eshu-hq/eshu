// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"strings"
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
			// The leak check runs FIRST, and fatally. Behind the exact-output
			// assertion it could never fire: a leaking row fails that first and
			// t.Fatalf ends the subtest, so the one message naming a shipped
			// credential was dead code. The diff is also the wrong message for
			// a leak — it prints the body, which is exactly what checkSecret
			// refuses to echo.
			if err := tc.CheckFreeTextSecret(got); err != nil {
				t.Fatal(err)
			}
			// Kept, and second: it is what catches over-removal and a rewritten
			// escape, neither of which the leak check can see.
			if got != tc.WantFreeText {
				t.Fatalf("redactFreeText(%q)\n got %q\nwant %q", tc.Input, got, tc.WantFreeText)
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

// boundaryFreeTextSentinel is this file's synthetic credential. It is the tail
// of a value that used to be cut at an escape, so a walk that truncates the
// value leaves it behind and the test names which escape did it.
const boundaryFreeTextSentinel = "FREETEXT-TERMINATOR-7c31d8"

// TestRedactFreeTextKeepsAnEscapedTerminatorInsideALiteralValue covers the half
// of the terminator set the shared corpus cannot reach. Whitespace, quotes and
// a backtick bound a value in prose but are not urlredact.PairSeparators, so a
// corpus row cannot exercise them — and they broke the same way the separators
// did when the terminator scan became decode-aware.
//
// A pair joined by a LITERAL "=" is text at the depth the reporter typed. An
// escape inside its value is characters OF the credential, so the value has to
// run past it: "?token=aa%20bb" is a token whose value is "aa bb". Reading the
// escape as a terminator cut the credential there and shipped the tail into a
// bundle stamped share-safe.
func TestRedactFreeTextKeepsAnEscapedTerminatorInsideALiteralValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		note string
		want string
	}{
		{
			name: "escaped space",
			note: "curl 'https://h/x?token=aa%20" + boundaryFreeTextSentinel + "'",
			want: "curl 'https://h/x?[redacted]'",
		},
		{
			name: "escaped single quote",
			note: "curl 'https://h/x?token=aa%27" + boundaryFreeTextSentinel + "'",
			want: "curl 'https://h/x?[redacted]'",
		},
		{
			// No colon outside the scheme, so the header rule stays out of the
			// way and the "%22" is what the value has to run past.
			name: "escaped double quote",
			note: `see "https://h/x?api_key=aa%22` + boundaryFreeTextSentinel + `"`,
			want: `see "https://h/x?[redacted]"`,
		},
		{
			name: "two escaped ampersands in one value",
			note: "curl 'https://h/x?token=aa%26bb%26" + boundaryFreeTextSentinel + "'",
			want: "curl 'https://h/x?[redacted]'",
		},
		{
			name: "an escaped pair still ends at its escaped terminator",
			note: "curl 'https://h/x?a%3D1%26token%3D" + boundaryFreeTextSentinel + "%26repo%3Ddemo'",
			want: "curl 'https://h/x?a%3D1%26[redacted]%26repo%3Ddemo'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, changed := redactFreeText(tt.note)
			if !changed {
				t.Fatalf("redactFreeText(%q) reported no change", tt.note)
			}
			if got != tt.want {
				t.Fatalf("redactFreeText(%q)\n got %q\nwant %q", tt.note, got, tt.want)
			}
			if strings.Contains(got, boundaryFreeTextSentinel) {
				t.Errorf("the credential survived the walk: %q", got)
			}
			// Capture runs Validate over its own output, so a second pass has
			// to be a no-op or a redacted bundle fails its own gate.
			if again, _ := redactFreeText(got); again != got {
				t.Fatalf("redactFreeText is not idempotent: second pass on %q gave %q", got, again)
			}
		})
	}
}
