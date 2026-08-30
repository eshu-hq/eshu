// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package containerimagetools defines pure route selection for the MCP
// container-image identity family.
//
// Route decides whether this package owns a tool and maps decoded arguments to
// a dependency-neutral internal request without executing it. The parent mcp
// package owns tool registration and its order, global route fanout, the
// private adapter, HTTP dispatch, authorization, timeouts, response budgets,
// envelopes, summaries, and telemetry. The query package owns the bounded
// reads behind the paths: which digest a deployed image reference resolves to,
// how a repository:tag's digest changed over time, and the counted and grouped
// summaries of the same identity facts.
//
// Four tools travel together here, and they do not share one shape. Three sit
// under /api/v0/supply-chain/container-images/identities; tag history is
// mounted at /api/v0/images/tag-history, a different prefix that must not be
// normalized onto the other. The identity listing pages by the
// after_identity_id cursor with a limit defaulting to 50; tag history pages by
// offset with the same limit default; the inventory pages by offset with a
// limit defaulting to 100 and a group_by falling back to outcome; and the
// count route carries no paging key at all, because the handler reads none:
// a limit sent there would be inert, not enforced.
//
// Required keys differ per route and the handlers enforce them. The identity
// listing 400s without limit and without at least one of digest, image_ref,
// source_repository_id, repository_id, or outcome, though a scoped token with
// no grants gets an empty page before the anchor is checked. Tag history 400s without
// both repository_id and tag, and its repository_id must carry the
// oci-registry:// prefix. The two aggregates require nothing, so a filter
// dropped there returns 200 over a wider scope than the caller asked for.
package containerimagetools
