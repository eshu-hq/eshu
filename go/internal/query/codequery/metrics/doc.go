// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package metrics implements the call-graph metrics read for the
// code-family queries: one indexed CALLS edge pass shared by the
// hub-function metrics, the recursive-function metrics, and the
// graph-summary packet's hot-entity ranking. It split out of package
// codequery (#6060 naming follow-up) so the hot read can be read,
// tested, and changed without pulling in the rest of the code surface,
// and so it can one day move into its own repo.
//
// The split keeps (*CodeHandler).CallGraphMetricsData in codequery --
// the method stays byte-identical there with its source_sha256 pin (see
// go/internal/queryplan/testdata/query-source-coverage.yaml) and calls
// this leaf through a thin forwarder. The builder text itself is pinned
// by cypher_sha256 in
// go/internal/queryplan/testdata/handler-hot-cypher.yaml: keep one text
// for all callers (a grant predicate here would be redundant -- every
// route binds the grant before the read -- and would fork the plan).
//
// Import discipline: this package may import the same dependency-neutral
// leaves codequery uses but NEVER package codequery itself and NEVER
// root package query -- both would create an import cycle (codequery
// calls this leaf, and root aliases codequery).
package metrics
