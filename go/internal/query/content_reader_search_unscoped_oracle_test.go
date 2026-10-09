// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/search/unscoped"
)

// TestOracleStatementIsDerivedFromShippedTailText pins, without a database, that
// the differential oracle is the shipped tail statement with only the old
// OFFSET appended: the same SELECT, WHERE and ORDER BY text, byte for byte, so
// the live differential compares the walk against the statement it replaced
// and the two cannot drift apart silently.
func TestOracleStatementIsDerivedFromShippedTailText(t *testing.T) {
	t.Parallel()

	shipped := strings.TrimSpace(unscoped.Statements().TailFirst)
	oracle := oracleOffsetSQL(t)
	if !strings.HasPrefix(oracle, shipped) {
		t.Fatalf("oracle is not a byte prefix extension of the shipped tail statement:\n%s", oracle)
	}
	if got, want := strings.TrimPrefix(oracle, shipped), " OFFSET $3::bigint"; got != want {
		t.Fatalf("oracle adds %q to the shipped text, want only %q", got, want)
	}
	for _, part := range []string{"FROM content_files", "content ILIKE '%' || $1 || '%'", "ORDER BY repo_id, relative_path", "LIMIT $2::bigint"} {
		if !strings.Contains(oracle, part) {
			t.Errorf("oracle lost %q", part)
		}
	}
}
