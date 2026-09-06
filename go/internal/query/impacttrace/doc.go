// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package impacttrace holds the non-method helpers behind the impact
// handler family (Issue #6060, lane B): deployment-trace readers,
// live-evidence probes, anchors, bounds, and response shapers that declare
// no ImpactHandler methods. The impact package imports impacttrace for these
// helpers, never the reverse; neither imports the query root.
package impacttrace
