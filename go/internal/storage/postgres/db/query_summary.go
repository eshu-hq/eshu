// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

import "context"

// querySummaryKey carries a bounded read name for the Postgres query a caller
// is about to run.
type querySummaryKey struct{}

// WithQuerySummary labels ctx with a bounded read name. InstrumentedDB stamps
// it on the postgres.query span as db.query.summary.
func WithQuerySummary(ctx context.Context, summary string) context.Context {
	return context.WithValue(ctx, querySummaryKey{}, summary)
}

// QuerySummaryFromContext returns the read name WithQuerySummary set, or "".
func QuerySummaryFromContext(ctx context.Context) string {
	summary, _ := ctx.Value(querySummaryKey{}).(string)
	return summary
}
