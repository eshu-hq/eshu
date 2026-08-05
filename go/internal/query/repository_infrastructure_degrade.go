// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "log/slog"

// infrastructureReadDegradedReason is the shared limitations/partial_reasons/
// failure_class value every caller uses when an infrastructure graph read
// fails but the response still answers 200 (#5764). infrastructure is a
// genuine auxiliary panel -- callers degrade rather than fail the whole
// context/story/workload response -- but the degradation must stay visible
// through a stable, named reason instead of being silently indistinguishable
// from "this repository has no infrastructure".
const infrastructureReadDegradedReason = "infrastructure_read_degraded"

// infrastructureTruncatedReason is the shared limitations/partial_reasons
// value every caller uses when a HEALTHY infrastructure graph read landed
// past its LIMIT bound -- more rows exist beyond it (P2-2 follow-up to
// #5764). Distinct from
// infrastructureReadDegradedReason: a truncated read still returned real
// rows, it just cannot rule out more past the bound, whereas a degraded read
// returned none because the read itself failed. The two are never both
// present for the same read.
const infrastructureTruncatedReason = "infrastructure_truncated"

// infrastructureDegradeLogAttrs builds the stage-log attributes a
// repositoryQueryStageTimer/serviceQueryStageTimer Done call attaches after an
// infrastructure read: row_count and truncated always, plus a bounded
// failure_class only when the read degraded, mirroring the #5761
// failure_class convention.
func infrastructureDegradeLogAttrs(rowCount int, degraded bool, truncated bool) []slog.Attr {
	attrs := repositoryContextDegradeLogAttrs(rowCount, degraded, infrastructureReadDegradedReason)
	return append(attrs, slog.Bool("truncated", truncated))
}

// repositoryContextDegradeLogAttrs builds the stage-log attributes for a
// repository-context auxiliary read that degrades instead of failing the
// whole response on a graph-read error (#5764, the infrastructure panel):
// row_count always, plus a bounded failure_class only when the read
// degraded. Infrastructure answers 200 either way, so this stage log -- not
// a new response field -- is the mechanism that keeps a degraded auxiliary
// read from being silent, mirroring the #5761 failure_class convention.
func repositoryContextDegradeLogAttrs(rowCount int, degraded bool, failureClass string) []slog.Attr {
	attrs := []slog.Attr{slog.Int("row_count", rowCount)}
	if degraded {
		attrs = append(attrs, slog.String("failure_class", failureClass))
	}
	return attrs
}
