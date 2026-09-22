// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package collector holds the collector-fleet family of the status report:
// backpressure, fact-evidence, generation dead letters, the readiness
// catalog, promotion proofs, unified runtime status, and vulnerability
// source checkpoints. The root internal/status package aggregates this
// package into RawSnapshot and Report, so the root imports collector; this
// package must never import the root or a sibling leaf. The one declared
// exception is cloud: collector imports cloud to fold AWSScanStatus rows
// into RuntimeStatus, because AWS cloud-scan evidence is one of the inputs
// the unified collector runtime view merges.
//
// BackpressureSnapshot reports bounded workflow-claim pressure for one
// collector family/instance/source-system tuple (pending, claimed, retrying,
// dead-lettered, terminal-failed, expired, active/overdue claims, and the
// oldest ages in each state). It deliberately excludes scope ids, source
// locators, generation ids, payload excerpts, and failure messages so the
// operator surface stays credential-safe.
//
// FactEvidence summarizes persisted source or reducer fact evidence for one
// collector runtime (observation count, last-observed/updated timestamps,
// bounded source-system names) without exposing source payload identifiers.
//
// GenerationDeadLetterSnapshot captures collector generation commit failures
// quarantined before normal projector/reducer work items existed.
//
// CatalogEntry, DefaultCatalog, and KnownKinds describe the readiness
// catalog: the deterministic spine of the promotion-proof report. Every
// catalog entry produces at least one proof row even when no instance is
// configured, so an unconfigured lane is explicit rather than silently
// absent. DefaultCatalog mirrors scope.AllCollectorKinds so a new collector
// kind automatically gets a synthesized readiness lane.
//
// RuntimeStatus is the unified operator view of one collector runtime
// identity, merged from workflow-coordinator registration, AWS cloud-scan
// evidence, vulnerability-source evidence, and persisted fact evidence.
// RuntimeStatuses derives it without I/O. PromotionProof and PromotionProofs
// derive the per-collector-family readiness verdict (implemented, partial,
// failed, stale, gated, disabled, permission_hidden, or unsupported) from a
// RuntimeStatus and the catalog, following a fixed precedence so a reviewer
// always sees the most actionable blocker first.
//
// VulnerabilitySourceState captures one durable vulnerability-intelligence
// source checkpoint for operator status and API readiness surfaces.
//
// RuntimeStatuses and PromotionProofs are collector-fleet business logic
// that read coordinator, AWS-scan, vulnerability-source, and fact-evidence
// data every one of which the root aggregates into Report. Because this
// package cannot import the root Report type, the root keeps thin,
// Report-shaped forwarders in status/compat_collector.go: it unpacks the
// Report fields this package's functions need and calls the collector-native
// entry point. External callers (internal/query's collector-readiness and
// evidence-bundle surfaces) keep calling the root-level name unchanged.
//
// The *JSON projections in this package (BackpressureJSONRows,
// RuntimeStatusesJSON, PromotionProofsJSON, VulnerabilitySourcesJSON) are the
// operator-facing wire contract: their struct tags are published API
// consumed by the status HTTP surfaces and MCP status tools, and are locked
// by byte-for-byte goldens at internal/status/testdata/render_json_golden.json,
// render_text_golden.txt, and render_json_key_paths_golden.txt. A golden
// failure is an API break to justify, not a golden to regenerate reflexively.
package collector
