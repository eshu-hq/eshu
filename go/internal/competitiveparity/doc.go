// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package competitiveparity validates that shipped Eshu capability surfaces
// remain reachable and documented against a peer-inspired parity checklist.
//
// The gate is intentionally offline. Callers supply CLI command paths, generated
// API/MCP/console surface names, public documentation text, and local exercise
// results. Validate scores that inventory against the default #3265/#3306
// expectations for first-run reports, operator digests, investigation evidence
// packets, and the capability catalog. The report separates reachability checks
// from deterministic usefulness scoring so a present-but-weak artifact can fail
// the gate, then renders as JSON or Markdown.
package competitiveparity
