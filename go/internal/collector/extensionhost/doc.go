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
// refuses any image reference that is not digest-pinned. Both also implement
// the optional PagedRunner contract: on collector-sdk/v1alpha3 the Source
// re-invokes the runner with each page's resume cursor until a final page
// closes the generation, then validates and commits the reassembled result
// as one. Runners without paged support keep the legacy single-call path,
// which fails a result carrying a page envelope terminal as invalid_result
// instead of committing one page as the whole generation. A paged runner
// returns the page on CallResult.Page; a page on the embedded SDK result fails
// the same way.
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
//
// When Config.ClaimOutcomeRecorder is set, the Source reports one
// ClaimOutcome per claim on every exit path and, for a completed claim, the
// same outcome again from ObserveClaimedGenerationCommitted once
// ClaimedService has durably committed or resolved the generation (#9420).
// The vulnerability-intelligence host uses it to write
// vulnerability_source_states; an attempt-row write error fails the claim
// retryable with FailureClassSourceStateRecord.
package extensionhost
