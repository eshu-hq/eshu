// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package semanticcode builds semantic.code_hint fact envelopes from
// already-redacted, already-parsed provider output.
//
// It is the code-hint twin of package semanticdocs, and it exists because
// semantic.code_hint had a read contract, a payload schema, an MCP tool, a
// capability row and a retention policy — and no producer anywhere in the
// runtime (issue #5693, from the #5552 deployed proof). Every deployed
// GET /api/v0/semantic/code-hints returned an empty list, correctly formed,
// which is indistinguishable from "this repository has no hints".
//
// # What this package is not
//
// It is not an LLM integration and it does not call a provider. Like
// semanticdocs, its input boundary (HintInput) carries only output that has
// already been produced, parsed and redacted somewhere else: no prompts, no
// provider request or response bodies, no credentials. What this package owns
// is the part that must not vary between providers — the provenance envelope,
// the stable fact identity, the policy/redaction/freshness state, and the
// non-canonical promotion boundary a code hint must carry to be admissible.
//
// # The promotion boundary
//
// A code hint is evidence, never truth. Every payload this package builds
// carries PromotionPolicy = requires_deterministic_evidence and an explicit
// CorroborationState, so a hint cannot be read as a canonical relationship no
// matter how confident the provider was. That boundary is enforced by
// facts.ValidateSemanticCodeHintPayload, which this package runs on every
// payload before emitting it: a hint that cannot state its corroboration
// state is a hint that does not ship.
package semanticcode
