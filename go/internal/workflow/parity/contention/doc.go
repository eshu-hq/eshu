// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package contention is the executable harness for the deployed-binary
// claim-plane contention proof (#9727, REMOVAL_CONTENTION_SPEC.md in
// go/cmd/workflow-coordinator).
//
// Run drives the spec's nine-step interleaved mix (claim, heartbeat,
// complete/release/fail, reap, quota, guarded admission, commit pin, replay,
// stale writers) from W concurrent claim loops against one parity.Backend leg:
// the in-tree WorkflowControlStore on a disposable local database, or the
// satellite claim plane over HTTP (a local container, or the live dark claim
// plane through a port-forward under the standing synthetic-write grant).
//
// Every id the run writes starts with Prefix. LoadSpec fails closed on any
// blank spec §2 field. The §7 abort triggers are enforced in code by two
// independent detectors: a client-side tracker that orders events only by
// happens-before, and a database sampler (Observer) that checks dual live
// claims, row loss, fencing tokens, and non-synthetic row changes. An abort
// cancels all workers and always runs the §8 reversal: bounded drain wait,
// prefix-only child-first batched deletes (never TRUNCATE), and the
// three-zero residue check. Run returns a Report (JSON plus a human summary)
// on every exit path.
//
// The package is test harness code: no production binary imports it, and it
// moves no authority. Its env-gated entry points are TestContentionInTree and
// TestContentionSatellite.
package contention
