// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package group holds the shared advisory-group projection contract
// for issue #10411: the grouped advisory membership the reducer computes and
// the graph writer reconciles.
//
// An advisory group gathers the (repository, package) edges that share one
// underlying advisory identity, so the graph answers "which repositories are
// affected by this advisory" without re-grouping CVE and GHSA rows on every
// read. The reducer owns grouping; the storage/cypher edge writer owns graph
// reconciliation; a sibling handler (added separately) owns the projection
// loop. Every lane imports these types so the group shape cannot drift
// between them.
//
// A Group carries no evidence stamps by design: membership rows assert which
// advisories cover which packages, not which collector observed them. The
// graph writer therefore writes only id, cve_ids, advisory_ids, and
// member_count on the node, and only ecosystem and purl on each
// AFFECTED_BY edge.
package group
