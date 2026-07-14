// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestSharedIntentStorePendingDomainContinuationUsesStrictTupleCursor(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"(created_at, intent_id) > ($2, $3)",
		"ORDER BY created_at ASC, intent_id ASC",
		"LIMIT $4",
	} {
		if !strings.Contains(listPendingDomainIntentsAfterSQL, want) {
			t.Fatalf("pending-domain continuation query missing %q", want)
		}
	}
}
