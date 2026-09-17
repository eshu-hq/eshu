// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package supplychainimpacttools defines pure route selection for the MCP
// supply-chain-impact family.
//
// Route decides whether this package owns a tool and maps decoded arguments
// to a dependency-neutral internal request without executing it. The parent
// mcp package owns tool registration and its order, global route fanout, the
// private adapter, HTTP dispatch, authorization, timeouts, response budgets,
// envelopes, summaries, and telemetry. The query package owns the bounded
// reads behind the paths: the reducer-owned vulnerability finding list, its
// whole-scope count and grouped inventory, and the single bounded explanation
// of why one package, image, or workload is or is not affected.
//
// Four tools travel together here, and they answer differently. The listing
// pages by the after_finding_id cursor with a limit defaulting to 50 and
// requires either that limit or one of its many scope filters -- cve_id,
// advisory_id, package_id, repository_id, subject_digest, image_ref,
// impact_status, ecosystem, workload_id, service_id, environment, severity,
// priority_bucket, or a positive min_priority_score. The count and the
// inventory share the same eighteen filters, carry no scope requirement at
// all, and answer whole-scope totals; the inventory adds a group_by falling
// back to impact_status, a limit defaulting to 100, and an offset defaulting
// to 0, while the count carries no paging key because its handler reads
// none. The explanation carries none of the listing's paging keys: it
// requires finding_id alone, or an advisory/CVE anchor plus one bounded scope
// leg, and answers exactly one finding, one no-evidence explanation, or one
// ambiguous-scope refusal.
package supplychainimpacttools
