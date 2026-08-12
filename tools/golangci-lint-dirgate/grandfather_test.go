// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Sanity checks over the generated grandfather.go ledger. These guard the
// INVARIANTS the ledger must hold, not its exact current membership --
// scripts/test-generate-dirgate-grandfather-go.sh is what proves
// grandfather.go matches scripts/lib/dirgate-grandfather.tsv byte for byte.
package main

import (
	"regexp"
	"testing"
)

var hexDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestGrandfatheredDirectoriesInvariants(t *testing.T) {
	if len(grandfatheredDirectories) == 0 {
		t.Fatal("grandfatheredDirectories is empty; the #6054 landing snapshot should not be")
	}
	for dir, entry := range grandfatheredDirectories {
		if dir == "" {
			t.Fatal("grandfatheredDirectories has an empty directory key")
		}
		if dir[len(dir)-1] == '/' {
			t.Errorf("grandfatheredDirectories[%q]: directory keys must not have a trailing slash", dir)
		}
		if entry.FileCount <= maxDirFiles {
			t.Errorf("grandfatheredDirectories[%q].FileCount = %d, want > %d (it must actually be an offender)", dir, entry.FileCount, maxDirFiles)
		}
		if !hexDigest.MatchString(entry.Digest) {
			t.Errorf("grandfatheredDirectories[%q].Digest = %q, want a 64-char lowercase hex sha256", dir, entry.Digest)
		}
	}
}
