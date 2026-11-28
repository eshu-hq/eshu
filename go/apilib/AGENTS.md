# apilib — Agent Instructions

Scope: `go/apilib/` (module `github.com/eshu-hq/eshu/go/apilib`) plus
the `admin/` leaf. Pre-1.0 internal audience (Refs
eshu-hq/eshu-api#456).

## Ownership

This module owns the proof-read surface only: readiness plus the two
admin proof reads (tuning-report, work-items/query). Core delegates its
proof handlers here; core keeps the fence adapter, auth, and all other
admin routes.

- Do not add handlers, routes, or store methods beyond the proof reads.
  Widening needs a new extraction decision.
- Do not re-export this module to external consumers.

## Import Direction

- Stdlib only. This module MUST NOT import `go/internal` or any other
  in-repo module — the boundary is enforced by the `go-apilib` CI job
  (`go list -deps` must show no `eshu-hq/eshu/go/internal`).
- The `admin` leaf MUST NOT import the parent in ways that widen the
  surface; mounts stay per-family (`MountProofReads`,
  `MountReadiness`), never a full mux. Mount funcs take no credential:
  the caller MUST wrap them in its auth chain.

## Failure Answers

- Nil store → 503 `{error, detail}`; bad JSON → 400; store error →
  fixed 500 `work-item query failed` (never `err.Error()`); client
  cancel → 499. The fence-verdict hook runs first; core passes its
  adapter, fixtures never produce fence sentinels.
- `/readyz` header is bare `application/json` (core-exact); tuning
  bodies use charset via the shared JSON shape. Do not "unify" them.
