// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package apicontract publishes the Eshu API wire shapes a thin client needs,
// without importing Eshu internals.
//
// It carries the response envelope, truth, and profile shapes from
// go/internal/query/querycontract (envelope.go, profile.go, freshness.go),
// the MCP tool-definition shape from go/internal/mcp/contract/tool, and the
// client-scoped environment-variable registry data plus its pure validator
// from go/internal/envregistry. Every JSON field name, tag, and enum value is
// pinned byte-for-byte to the core types by the committed goldens in
// testdata/golden: the core drift tests regenerate those goldens from the
// live types, and the in-module parity tests compare them against these
// shapes, so neither side can drift without failing a gate.
//
// What it deliberately does not publish, and why:
//
//   - Envelope constructors and the capability matrix (BuildTruthEnvelope,
//     capability ceilings, profile support tables): capability truth belongs
//     to the core query owner.
//   - Freshness cause plumbing (WithFreshnessCause, the cause to next-check
//     table, timestamp formatters): the server attaches causes; a client only
//     decodes them.
//   - Retry behavior markers, rendering and composition (packet renderers,
//     report composers, bundle capture/validate, markdown reference pages):
//     those run behind core endpoints, never in a client library.
//   - Answer classification prompts (AnswerTruthClass): a prompt-surface
//     derivation, not wire truth.
//   - Server, storage, reducer, graph, queue, and supervisor surfaces: never
//     a client contract.
//
// Sync rule: a core change to any ported shape or entry must update the
// goldens and this package in the same change; the drift tests fail closed
// until it does. Registry gaps the client reads but the core registry does
// not cover yet (ESHU_SERVICE_URL, ESHU_REMOTE_TIMEOUT_SECONDS) stay out of
// ClientEntries until the registry owns them; see README.md.
package apicontract
