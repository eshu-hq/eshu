// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package materialization reduces one parser relationship follow-up into
// durable shared-intent emission for code-call, Python metaclass, and
// symbol→runtime (HANDLES_ROUTE/RUNS_IN/INVOKES_CLOUD_ACTION) rows (issue
// #6061). [Handler.Handle] loads repository/file facts, extracts code-call
// and metaclass rows through the sibling [codecall] package, and builds the
// symbol→runtime rows in-package through [BuildIntentRows] (routes.go,
// workloads.go, cloud_actions.go), which resolves parser-owned framework
// route handlers, deployed-runtime bindings, and AWS SDK call sites to exact,
// unambiguous Function entities via the shared [shared.EntityIndex]
// ([codecall.ExtractAllRelationshipRowsWithIndex]).
//
// [ExtractIntentRows] (refresh.go) is the backend-free seam: it builds the
// entity index and projection contexts and calls [BuildIntentRows] without
// needing a graph backend, Postgres, or a clock, so callers outside the
// reducer tree (internal/ifa, the mcp/query route-to-caller proof) can derive
// symbol→runtime rows from real production logic instead of hand-inventing
// literals that could silently diverge.
//
// The reducer root imports this package as materialization. It keeps the
// exported CodeCallMaterializationHandler/CodeCallIntentWriter/
// ExtractSymbolRuntimeIntentRows/BuildHandlesRouteIntentRowsForQueryProof
// spellings through the code-call stanza of compat_projection.go.
//
// Dependency rule: from the reducer tree this package imports only the
// shared tier (contract, factdecode, factload, payloadcore, schemadecode,
// sharedintent, iamcan) and the sibling leaves codecall and
// code/call/shared; outside it, facts, internal/codeprovenance, and the
// standard library. It never imports the reducer root.
package materialization
