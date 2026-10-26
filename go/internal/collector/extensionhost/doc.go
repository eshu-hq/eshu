// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package extensionhost runs out-of-tree collector SDK extensions through the
// same claim-aware collector intake used by first-party hosted collectors.
//
// Callers provide an already-claimed workflow item, a component manifest, and a
// host-owned runner; the package returns collected generations or classified
// failures for collector.ClaimedService. It validates SDK output and
// manifest-declared payload schema references before mapping facts, rejects
// returned claim identity mismatches, and keeps workflow claim mutation,
// stale-fence handling, and commits outside the extension boundary.
//
// Two runners implement the Runner contract: ProcessRunner launches a local
// process (development and first-party use), and OCIRunner launches a
// digest-pinned OCI artifact under container isolation (no network by default,
// read-only root filesystem, non-root user, all capabilities dropped, no new
// privileges, and no Eshu datastore/graph/reducer/API/MCP/workflow handles).
// Both speak the same bounded JSON SDK contract over stdin/stdout; OCIRunner
// refuses any image reference that is not digest-pinned.
//
// The host is the owning decision site for the activation and emission stages
// of producer-grant admission. Config.GrantObserver receives one allow or deny
// decision per core-owned fact kind, and GrantTelemetry turns those decisions
// into the eshu_dp_component_producer_grant_decisions_total counter, a span
// event, and structured log keys without ever carrying credentials, grant
// scope, or fact payloads. Observation is additive: a denied grant still fails
// the result closed.
//
// StatusTelemetry is the production StatusRecorder (#8908). Through the
// optional ResultRecorder interface the Source reports one ResultRecord per
// claim, for every SDK state and host verdict, and StatusTelemetry turns status
// rows and results into the eshu_dp_component_extension_* instruments and one
// bounded log line, never labeling a scope, instance, or URL. A retryable
// result's classified failure carries the largest status retry_after_seconds,
// clamped to MaxRetryAfterDelay, as RetryAfterDelay for ClaimedService pacing.
//
// When Config.TokenProvider is set, the host mints one GitHub App installation
// token per claim and injects it into the request config under
// HostMintedGitHubTokenKey; only the minted token crosses the extension
// boundary, never the App private key. A mint denial fails the claim terminal
// with a bounded auth_denied failure before the runner launches, while a
// transient mint fault stays retryable.
package extensionhost
