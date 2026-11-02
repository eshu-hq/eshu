// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package media

import (
	"strings"
	"testing"
)

// TestLegacyKeywordsAreTheSixOriginalEntries pins the source-name list as a
// literal. It stays equal to identityMarkers in document/ocr, and a change to
// one lands in both.
func TestLegacyKeywordsAreTheSixOriginalEntries(t *testing.T) {
	t.Parallel()

	want := []string{"credential_marker", "secret_marker", "token_marker", "password", "api_key", "private_key"}
	if strings.Join(legacyKeywords, "|") != strings.Join(want, "|") {
		t.Fatalf("legacyKeywords = %q, want %q", legacyKeywords, want)
	}
}
