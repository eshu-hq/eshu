// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidredact

import (
	"strings"
	"testing"
)

const tokenTestSecret = "eshu_live_SUPERSECRET_abc123def456ghi789"

func TestRedactTokenNeverReturnsRaw(t *testing.T) {
	t.Parallel()
	got := RedactToken(tokenTestSecret)
	if got == tokenTestSecret {
		t.Fatal("RedactToken returned the raw token")
	}
	if strings.Contains(got, "SUPERSECRET") {
		t.Fatalf("RedactToken leaked secret body: %q", got)
	}
	if !strings.HasPrefix(got, "eshu") {
		t.Fatalf("RedactToken should keep a short recognizable prefix, got %q", got)
	}
	if RedactToken("") != "" {
		t.Fatal("RedactToken(\"\") should be empty")
	}
	if got := RedactToken("short"); strings.Contains(got, "short") {
		t.Fatalf("short token not fully masked: %q", got)
	}
}
