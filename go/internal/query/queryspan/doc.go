// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package queryspan owns the per-route span that query HTTP reads emit.
//
// It exists so a handler-family subpackage under go/internal/query can start the
// same span without importing the root query package, which it cannot do
// without an import cycle through root's compatibility aliases (#6060). The
// instrumentation-scope name stays "eshu/go/internal/query" regardless of where
// the code sits, because saved span queries and dashboards match on that name.
//
// Callers pass their own tracer to StartHandlerSpanWith rather than the package
// reading a shared one. That keeps a test's recording provider private to the
// package that installed it: a swap in one family cannot change what another
// family records, and two such swaps cannot race.
package queryspan
