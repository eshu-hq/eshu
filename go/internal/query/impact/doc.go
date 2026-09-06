// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package impact holds the impact-analysis handler family (Issue #6060,
// lane B): the ImpactHandler HTTP surface plus every file that declares one
// of its methods, the pre-change check types, and the exported seam the
// staying root package consumes through aliases. Non-method helpers that the
// family needs but do not touch handler state live in impacttrace.
package impact
