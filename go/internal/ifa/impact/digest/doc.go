// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package digest is the Postgres-side truth read of the supply-chain impact family of the Ifá
// fault and determinism gates (#10880 PR-C2).
//
// The impact domain writes no graph, so the graph-dump digest the Compose cells compare cannot see it.
// Its output is two tables: one support row per finding and one canonical winner per finding key.
// DigestSQL hashes both, without the columns that name the delivering run or write (the intent id and cause
// inside a support payload, the winners' materialization stamp, and the per-write observation timestamp
// provenance.first_seen_at on both), so two runs of the same input at any worker count hash equal. The in-process family in go/internal/storage/postgres and the
// Compose cell's `ifa impact-digest` verb read the same statement through Read, so they cannot drift.
// Cassettes names the committed cassettes the family drives, in commit order.
package digest
