// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package parity provides a fixture-to-runtime parity harness for claim-driven
// collectors.
//
// The harness drives the real collector.ClaimedService claim/commit path with
// fully in-memory fixtures (no live-provider credentials) and verifies each
// fixture generation against the same reducer readback contract hosted
// collectors must satisfy:
//
//   - stable-key idempotency: replaying a generation does not duplicate rows;
//   - fencing-token supersede: a stale (lower-token) generation never overwrites
//     readback;
//   - withheld evidence: permission-hidden and unsupported facts are committed
//     as evidence but never become readable;
//   - failure routing: a commit failure records a generation dead-letter and
//     routes the claim to retryable or terminal, with attempt-budget exhaustion
//     escalating to terminal;
//   - replay clearing: a successful replay of a previously dead-lettered scope
//     completes the dead-letter.
//
// Build scenarios with NewScenario plus the AdmissibleFact, PermissionHiddenFact,
// and UnsupportedFact helpers, then run them through a Harness. The harness
// reuses one stateful readback model across runs so duplicate delivery, stale
// generations, and dead-letter replay can be modeled as scenario sequences.
//
// Run.Err and Result report whether each scenario met its declared contract; the
// harness fails when fixture facts cannot reach the expected reducer/readback
// contract. Summarize aggregates results into a per-collector FixtureReadiness
// verdict that feeds the per-collector promotion proof report: a fixture lane
// that cannot reach readback must not be promoted to live readiness on fixture
// evidence alone.
package parity
