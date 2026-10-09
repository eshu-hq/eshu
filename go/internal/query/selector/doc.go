// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package selector resolves a caller-supplied repository selector to a
// canonical repository id, under the caller's authorization bounds.
//
// A selector is whatever a client typed: an id, a name, a path, a local path, a
// remote URL, or a slug. ResolveExactForAccess matches it against the graph and
// the content catalog, filters by the supplied access bounds, and returns
// exactly one id or a typed NotFoundError or AmbiguousError. When a backing
// read fails instead (the catalog read or either graph read), it returns a
// LookupError that wraps the backend error, so errors.Is still reaches the
// reader-fence and graph-availability sentinels; IsLookupFailure matches it.
// LookupError text never carries the selector.
//
// ResolveForRequestWithAccess wraps that for HTTP handlers, writing the stable
// error contract and reporting whether the caller should continue: a fence or
// graph-availability verdict answers 503/504 through
// querycontract.WriteGraphReadError, any other LookupError answers 500 with the
// fixed LookupFailureMessage body and records the error on the request span,
// NotFoundError answers 404, AmbiguousError answers 400, and any other error
// answers the same fixed 500 as a LookupError (#7674). A caller that maps
// selector errors itself calls WriteLookupFailure after WriteGraphReadError, so
// it answers the same 500, fixed body, and span error, never the error text,
// which carries backend detail. Both answer through tracing.WriteServerFailure,
// so a lookup that failed because the caller canceled its own request answers
// 499 with only the client-cancel span event instead.
//
// HydrateResolvedEntityRepoIdentity hydrates an already-resolved entity's own
// canonical repository identity (repo_id, repo_name) under the same access
// filter, rather than resolving a selector into one.
//
// It is its own package, rather than part of querycontract, because the
// request-level entry point writes to a ResponseWriter. Request-time
// orchestration does not belong in the dependency-neutral contract package.
package selector
