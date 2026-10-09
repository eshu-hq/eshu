// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package registry implements the package-registry query handler family:
// graph-backed package/version identity reads, package-native dependency
// edges, reducer-derived correlation reads, and graph aggregate/inventory
// reads, all served under Handler.
//
// It moved out of root package query into the flat internal/query/packagereg
// leaf (#6060), then nested under internal/query/package/registry with a
// matching export destutter (#6642 Part D) so the package clause reads
// registry.Handler instead of packagereg.PackageRegistryHandler. It depends
// only on the dependency-neutral leaf packages under internal/query --
// querycontract (ports, row-value decoders, response and truth envelopes,
// capability gates), decode (classified fact-decode failures), selector
// (repository-selector resolution), and tracing (the per-route HTTP span)
// -- never on root package query itself, which would create an import cycle:
// root's package_registry_alias.go imports this package for the
// Handler/CorrelationRow compatibility aliases cmd/api and cmd/mcp-server
// still use.
//
// This family's six capabilities stay registered in root package query
// (contract_package_registry.go, contract_capability_matrix.go), which owns
// the router and always links into the production binary. Because
// go test ./internal/query/package/registry cannot link root (the cycle
// above), main_test.go's TestMain registers the same six capabilities with
// querycontract before this package's own tests run, faithfully mirroring
// root's values; see that file's doc comment for why it exists and why it is
// not redundant.
//
// A failed graph, correlation, or aggregate read answers a fixed message per
// route step and records the backend error on the handler span; the error
// text never reaches the response body (#7674). A stale or timed-out
// PostgreSQL reader answers the retryable 503 through
// querycontract.WriteGraphReadError, and a client cancel answers 499 with no
// span error. A scoped gate probe failure answers that error status, never
// the empty page of a denied grant, so the gates fail closed.
package registry
