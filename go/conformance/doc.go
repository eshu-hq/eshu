// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package conformance is the out-of-tree contributor conformance suite for the
// Eshu deterministic replay framework (issue #4112 / R-10, epic #4102 §8). A
// contributor runs it from their own clone with:
//
//	cd go && go test ./conformance -count=1
//
// with zero provider credentials and zero Docker. It replays a committed starter
// cassette through the shared, credential-free cassette replay Source
// (internal/replay/cassette), derives the projected graph observation in memory
// (Observe), loads the starter spec YAML into the same goldengate.Snapshot
// contract the in-repo B-7 gate uses (LoadSpec), and evaluates the observation
// against it with the SAME goldengate.Evaluate* assertions (Evaluate) — there is
// no forked assertion logic.
//
// Because the assertions are shared with cmd/golden-corpus-gate via the
// internal/goldengate package, a green conformance run is the credential-free
// deterministic proof referenced by the #4047 monorepo-split readiness checklist
// as the automated proof for the collector extraction criterion: it shows a
// collector's facts project the node/edge/correlation truth the spec demands,
// reproducibly, without a live pipeline.
//
// The starter schema (Observe) maps neutral starter.* fact kinds — a repository,
// its directories, and its files — to Repository/Directory/File nodes and
// CONTAINS edges. A contributor swaps the cassette, the spec, and the Observe
// mapping for their own collector's fact kinds; the replay primitive, the spec
// contract, and the Evaluate* assertions stay shared and unchanged.
package conformance
