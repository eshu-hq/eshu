// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package schedulereplay is the Layer 3 (ordering) schedule replay for the
// deterministic replay framework (design doc 4102, R-13 / #4122).
//
// It drives recorded projection work through the real reducer service loop using
// a deterministic in-memory work source (ScheduledWorkSource) that delivers
// intents in a scripted order — in-order, adversarial reverse, rotated, and
// duplicate delivery — in place of the production FOR UPDATE SKIP LOCKED Postgres
// claim path. Each delivery order drains into an in-memory canonical graph; the
// gate asserts the converged Canonical snapshot is byte-identical across every
// order, proving the projection's final graph truth is delivery-order
// independent (the offline, credential-free analog of the B-12 snapshot).
//
// Work items come from the committed offline-tier cassette through the real
// cassette -> offlinetier materialization seam, so the inputs are recorded facts,
// not synthetic toys. Items reference shared node keys (a child directory's edge
// points at its parent's node), so reordering exercises the #4019
// child-before-parent conflict-key class rather than independent inserts. A
// deliberately order-sensitive applier is used in tests to prove the gate detects
// ordering bugs.
//
// The package requires no Postgres and no graph backend, so the ordering gate
// runs in the default `go test` pass on every PR.
package schedulereplay
