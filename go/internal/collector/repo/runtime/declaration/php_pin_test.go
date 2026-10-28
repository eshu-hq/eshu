// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import "testing"

func TestPHPVersionPin(t *testing.T) {
	t.Parallel()
	got := emitDecoded(t, "app/.php-version", "8.3.12\n")
	checkFacts(t, got, []wantFact{
		{"app", "php_version", "php", "<nil>", "8.3.12", "exact", "<nil>", 1, 0, 8, 3, 12},
	})
}

func TestPHPVersionPinRejectsCredential(t *testing.T) {
	t.Parallel()
	envelopes, skip := EmitFile(testContext(".php-version"), "repo-1", ".php-version", "0123456789abcdef-secret\n")
	if len(envelopes) != 0 || skip.Reason != SkipReasonValueRejected || skip.Rejected != 1 {
		t.Errorf("credential-shaped pin emitted %d facts, skip %+v", len(envelopes), skip)
	}
}

func TestPHPVersionPinSpecShapesAndMalformedValue(t *testing.T) {
	for _, tc := range []struct{ raw, kind string }{
		{"8.3.12", "exact"}, {"^8.2", "range"}, {"latest", "alias"}, {"${PHP_VERSION}", "unresolved"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			got := emitDecoded(t, ".php-version", tc.raw+"\n")
			if len(got) != 1 || got[0].SourceKind != "php_version" || got[0].Runtime != "php" || got[0].RawSpec != tc.raw || got[0].SpecKind != tc.kind || got[0].LineNumber != 1 {
				t.Fatalf(".php-version %q declarations = %+v", tc.raw, got)
			}
		})
	}
	for _, tc := range []struct{ name, body string }{
		{"invalid UTF-8", "\xff\n"}, {"invalid punctuation", "8.2;secret\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			envelopes, skip := EmitFile(testContext(".php-version"), "repo-1", ".php-version", tc.body)
			if len(envelopes) != 0 || skip.Reason != SkipReasonValueRejected || skip.Rejected != 1 {
				t.Fatalf("malformed pin emitted %d declarations, skip %+v", len(envelopes), skip)
			}
		})
	}
}
