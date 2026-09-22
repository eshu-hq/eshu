// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package semantic holds the optional LLM-assisted semantic-extraction
// family of the status report: extraction liveness, its bounded queue,
// budget, and audit snapshots, and the redacted provider-profile roster that
// backs it. The root internal/status package aggregates this package into
// RawSnapshot and Report, so the root imports semantic; this package must
// never import the root or a sibling leaf.
//
// ExtractionStatus captures optional semantic-extraction liveness: state
// (unavailable, available, available_but_disabled_for_scope,
// disabled_by_policy, provider_unhealthy), a stable machine reason, a
// redacted human detail, the documentation-observations/code-hints
// enablement flags, and the queue/budget/audit snapshots below.
// ExtractionSupportedStates returns the closed state enum;
// DefaultExtractionStatus returns the zero-key unavailable status used when
// no provider is configured. Deterministic indexing, reducer projection, API
// reads, MCP tools, and documentation fact verification are unaffected by
// any extraction state — the doc comment on every non-available state says
// so explicitly, because operators read this status under pressure and must
// not infer a platform-wide outage from an optional LLM path being off.
//
// ExtractionQueueSnapshot, ExtractionBudgetSnapshot, and ExtractionAuditSnapshot
// capture aggregate queue lifecycle counts, redacted token/cost budget
// totals, and audit-safe actor/ACL class counts, respectively — all without
// raw prompts, responses, source identifiers, chunk hashes, principals, or
// provider responses.
//
// ProviderProfileStatus is the redacted operator view of one configured
// semantic-extraction provider profile: profile and provider identity,
// whether a credential is configured (never the credential itself), model
// and embedding metadata, allowed source classes, and health state.
// ProviderProfileSupportedStates returns the closed state enum
// (configured, unconfigured, healthy, unhealthy) — this package does not
// probe providers; health is asserted by an external source and normalized
// here.
//
// Only the profile type, its state constants, and the normalization/query
// helpers over it live here. The reader decorator that attaches a static
// profile roster to a live status read (WithSemanticProviderProfiles in
// root internal/status) wraps the root's Reader/RawSnapshot/SnapshotSelection
// contract and must stay in root — this package cannot depend on those root
// types. Root's decorator calls into this package's exported profile-clone
// helper to normalize the roster it attaches.
//
// The *JSON projections in this package (ExtractionStatusJSON,
// ProviderProfilesJSON, and the queue/budget/audit JSON helpers) are the
// operator-facing wire contract: their struct tags and formatting are part
// of the published status JSON consumed by the status HTTP surfaces and MCP
// status tools, and are locked by byte-for-byte goldens at
// internal/status/testdata/render_json_golden.json, render_text_golden.txt,
// and render_json_key_paths_golden.txt. A golden failure is an API break to
// justify, not a golden to regenerate reflexively.
package semantic
