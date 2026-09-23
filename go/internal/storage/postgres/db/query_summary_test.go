// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

import (
	"context"
	"testing"
)

func TestQuerySummaryRoundTripsThroughContext(t *testing.T) {
	t.Parallel()

	if got := QuerySummaryFromContext(context.Background()); got != "" {
		t.Fatalf("QuerySummaryFromContext(unlabeled) = %q, want empty", got)
	}

	ctx := WithQuerySummary(context.Background(), "active_work_summary")
	if got, want := QuerySummaryFromContext(ctx), "active_work_summary"; got != want {
		t.Fatalf("QuerySummaryFromContext(labeled) = %q, want %q", got, want)
	}
}
