// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"regexp"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestWinnerSelectReadsNoDigestExcludedPayloadField is the select side of the
// #9805 capture-filter soundness argument. The writer skips the capture of a
// rewrite whose content digest is unchanged, so the winners select must not
// read a payload key the digest leaves out. The digest side (every other key
// moves the digest) is TestContentDigestCoversEveryPayloadFieldExceptTheExcludedOnes.
func TestWinnerSelectReadsNoDigestExcludedPayloadField(t *testing.T) {
	t.Parallel()

	read := regexp.MustCompile(`payload\s*(?:->>|->)\s*'([a-z_]+)'|payload\s*#>>\s*'\{([a-z_]+)`)
	matches := read.FindAllStringSubmatch(postgres.WinnerSelectSQLForTest, -1)
	if len(matches) < 20 {
		t.Fatalf("found %d payload reads in the winners select, want the full set (the pattern is stale)", len(matches))
	}
	excluded := map[string]bool{}
	for _, key := range supplychaincore.DigestExcludedPayloadKeys() {
		excluded[key] = true
	}
	for _, m := range matches {
		key := m[1]
		if key == "" {
			key = m[2]
		}
		if excluded[key] {
			t.Errorf("the winners select reads payload key %q, which the content digest excludes: a rewrite that changes it would not be captured", key)
		}
	}
}
