// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package queryselector resolves a caller-supplied repository selector to a
// canonical repository id, under the caller's authorization bounds.
//
// A selector is whatever a client typed: an id, a name, a path, a local path, a
// remote URL, or a slug. ResolveExactForAccess matches it against the graph and
// the content catalog, filters by the supplied access bounds, and returns
// exactly one id or a typed NotFoundError or AmbiguousError.
// ResolveForRequestWithAccess wraps that for HTTP handlers, writing the stable
// error contract and reporting whether the caller should continue.
//
// It is its own package, rather than part of querycontract, because the
// request-level entry point writes to a ResponseWriter. Request-time
// orchestration does not belong in the dependency-neutral contract package.
package queryselector
