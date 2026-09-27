// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package oci holds the pure OCI registry-truth helpers behind the impact
// deployment-trace handler family (#6590): parsing image refs into digest-
// and tag-addressed keys, batching and row-limit bookkeeping for the
// tag-observation and image-by-digest reads, and shaping raw joined rows
// into truth rows. It issues no graph query itself -- the three Run-calling
// fetchers stay in go/internal/query/impact/trace_deployment_oci.go so the
// query-source-coverage registry keeps attributing rows to them, not to this
// helper package -- so every symbol here is exercised without a graph reader
// and without importing the query root or a graph driver. The impact package
// imports oci for these helpers, never the reverse.
package oci
