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
