// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package deployment holds the non-method helpers behind the impact
// handler family (Issue #6060, lane B): deployment-trace readers,
// live-evidence probes, anchors, bounds, and response shapers that declare
// no ImpactHandler methods. The impact package imports deployment for these
// helpers, never the reverse; neither imports the query root.
//
// ApplySectionSelection (#7174) shapes a built trace response by the
// caller's sections and evidence_detail: unselected families are deleted,
// "handles" projects primary-family rows to their identity keys, and every
// family not returned in full is reported in section_detail and returned as
// querycontract.TruthOmission values for truth.omissions. SectionNames is the
// single source of the selectable family names.
package deployment
