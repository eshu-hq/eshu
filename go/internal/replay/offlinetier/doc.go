// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package offlinetier holds the R-5 offline replay gate tier (epic #4102,
// issue #4107): an env-gated go test that replays a committed cassette through
// the REAL canonical projection writer into a REAL single-container NornicDB
// (no Docker Compose), then reads the graph back over Bolt and asserts node and
// edge truth.
//
// The tier exists to catch backend-specific projection bugs that only surface
// against a real graph engine — the #4019 phase-group executor within-phase
// read-your-writes / nested-directory drop, commit-time MERGE uniqueness races,
// and NornicDB MATCH quirks. A fake or in-memory graph cannot reproduce these,
// so this tier NEVER substitutes one: when no real backend is configured it
// SKIPs cleanly (it does not silently pass), and when a backend is present it
// fails on any mismatch.
//
// Activation is gated on ESHU_REPLAY_TIER_LIVE plus the standard Bolt/graph
// environment (ESHU_GRAPH_BACKEND, NEO4J_URI, ESHU_NEO4J_DATABASE). The
// companion scripts/verify-replay-tier.sh starts the lean NornicDB container
// with plain docker run, exports that environment, and runs the focused test.
//
// The tier reuses existing projection code unchanged: it drives
// storage/cypher.CanonicalNodeWriter (the production canonical projector write
// path) over a driver-backed executor, so it adds no new projection logic and
// carries no projection-path regression risk.
package offlinetier
