// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import "log/slog"

// infrastructureReadDegradedReason is the shared limitations/partial_reasons/
// failure_class value every caller uses when an infrastructure graph read
// fails but the response still answers 200 (#5764). infrastructure is a
// genuine auxiliary panel -- callers degrade rather than fail the whole
// context/story/workload response -- but the degradation must stay visible
// through a stable, named reason instead of being silently indistinguishable
// from "this repository has no infrastructure".
// InfrastructureReadDegradedReason is the shared limitations/partial_reasons
// value for a degraded infrastructure read. Exported for #6060 so root
// stayers outside the repository family can name it from outside this
// package.
const InfrastructureReadDegradedReason = "infrastructure_read_degraded"

const infrastructureReadDegradedReason = InfrastructureReadDegradedReason

// infrastructureTruncatedReason is the shared limitations/partial_reasons
// value every caller uses when a HEALTHY infrastructure graph read landed
// past its LIMIT bound -- more rows exist beyond it (P2-2 follow-up to
// #5764). Distinct from
// infrastructureReadDegradedReason: a truncated read still returned real
// rows, it just cannot rule out more past the bound, whereas a degraded read
// returned none because the read itself failed. The two are never both
// present for the same read.
// InfrastructureTruncatedReason is the shared limitations/partial_reasons
// value for a truncated infrastructure read. Exported for #6060; see
// InfrastructureReadDegradedReason.
const InfrastructureTruncatedReason = "infrastructure_truncated"

const infrastructureTruncatedReason = InfrastructureTruncatedReason

// infrastructureDegradeLogAttrs builds the stage-log attributes a
// repositoryQueryStageTimer/serviceQueryStageTimer Done call attaches after an
// infrastructure read (#5764, the infrastructure panel, shared by the
// repository-context/story and workload/service-context call sites): row_count
// and truncated always, plus a bounded failure_class only when the read
// degraded, mirroring the #5761 failure_class convention. Infrastructure
// answers 200 either way, so this stage log -- not a new response field -- is
// the mechanism that keeps a degraded auxiliary read from being silent.
func InfrastructureDegradeLogAttrs(rowCount int, degraded bool, truncated bool) []slog.Attr {
	attrs := []slog.Attr{slog.Int("row_count", rowCount)}
	if degraded {
		attrs = append(attrs, slog.String("failure_class", infrastructureReadDegradedReason))
	}
	return append(attrs, slog.Bool("truncated", truncated))
}

// infrastructureDegradeLogAttrs keeps the in-package spelling after the #6060
// export; root stayers name InfrastructureDegradeLogAttrs.
func infrastructureDegradeLogAttrs(rowCount int, degraded bool, truncated bool) []slog.Attr {
	return InfrastructureDegradeLogAttrs(rowCount, degraded, truncated)
}
