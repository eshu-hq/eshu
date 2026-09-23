// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package iacstore persists reducer-materialized IaC (infrastructure as
// code) reachability rows: for each active repository generation, one row
// per Terraform/Helm/etc. artifact records whether the platform graph found
// a modeled reference to it, and the operator-facing cleanup finding that
// follows from that reachability class.
//
// IaCReachabilityStore owns the durable table (its DDL, upsert batching,
// and the cleanup-finding reads used by the query and MCP surfaces). It is
// distinct from code reachability (call-graph reachability over source, a
// separate store family): this package answers "is this infrastructure
// artifact referenced by anything modeled", not "is this function called".
//
// The reducer-side materializer that computes these rows from content-file
// evidence (MaterializeIaCReachability) stays in the parent postgres
// package: it is a method on IngestionStore, which has not moved out of
// root yet. This package must not import the parent postgres package.
package iacstore
