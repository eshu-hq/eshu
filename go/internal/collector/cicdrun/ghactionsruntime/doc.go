// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ghactionsruntime collects bounded GitHub Actions run metadata for
// the CI/CD run collector family.
//
// The package owns hosted provider polling and claim resolution. It delegates
// fact envelope construction to the fixture-backed cicdrun normalizer so live
// provider rows and offline fixtures share one schema. HTTP responses are
// closed by the client after each bounded provider read. The package does not
// read artifact contents, logs, secrets, graph state, or query state.
//
// Every claim fetches a bounded, stateless window of the most recent runs
// (max_runs); re-fetching the same window on a later cycle is an idempotent
// upsert at projection, not a resume operation. An optional
// SourceConfig.Watermarks (runwatermark.Store) tracks the newest run ID a
// claim observed so a LATER claim can detect -- not resume -- a cross-cycle
// gap: when more than max_runs runs land between two cycles, runs between
// the previous watermark and the new window's floor were never fetched by
// either cycle. See run_watermark.go.
//
// The watermark only advances after a claim cycle's facts have durably
// committed: NextClaimed stashes the observed newest run ID
// (pending_watermark.go), and ClaimedSource.ObserveClaimedGenerationCommitted
// (source_commit_observer.go) persists it once collector.ClaimedService
// confirms the commit succeeded. This ordering fixes #5429: saving the
// watermark on NextClaimed's own success path, independent of whether the
// commit later succeeded, let a retried claim silently stop re-detecting a
// gap it had already correctly detected once.
//
// Each claim also fetches a bounded window of the target's GitHub
// Deployments API state (max_deployments, default 10, hard cap 100) when the
// configured Client implements DeploymentFetcher (#5425 STEP 3;
// client_deployments.go, source_deployments.go). The resulting
// ci.deployment_event facts are appended into the SAME CollectedGeneration
// as the ci.run facts above, not a later cycle: the reducer's correlation
// intent only forms for a generation containing a ci.run. DeploymentFetcher
// is optional so the package's many run-collection test doubles do not all
// need a FetchDeployments method.
package ghactionsruntime
