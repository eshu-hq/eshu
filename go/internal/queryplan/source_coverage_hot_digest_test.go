// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"strings"
	"testing"
)

func TestValidateSourceCoverageRejectsChangedHotCallsiteSource(t *testing.T) {
	manifest := Manifest{
		Version: 1,
		Entries: []Entry{{ID: "QP-HOT"}},
		SourceCoverage: []SourceCoverage{{
			File: "handler.go",
			Calls: []QueryCallsite{{
				Symbol:       "(*Handler).handle",
				Count:        1,
				EntryIDs:     []string{"QP-HOT"},
				SourceDigest: strings.Repeat("a", 64),
			}},
		}},
	}
	discovered := []SourceCoverage{{
		File: "handler.go",
		Calls: []QueryCallsite{{
			Symbol:       "(*Handler).handle",
			Count:        1,
			SourceDigest: strings.Repeat("b", 64),
		}},
	}}

	err := ValidateSourceCoverage(manifest, discovered)
	if err == nil || !strings.Contains(err.Error(), "hot callsite source_sha256 does not match production symbol") {
		t.Fatalf("ValidateSourceCoverage() error = %v, want hot callsite source drift", err)
	}
}
