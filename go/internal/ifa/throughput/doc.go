// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package throughput is Ifá's Layer 3 throughput Odù (issue #4579, parent epic
// #4389, ADR docs/internal/design/4389-ifa-conformance-platform.md "Layer 3").
//
// It amplifies one base Odù to a named scale-lab slot (go/internal/ifa's
// AmplifyAtSlot + ScaleSlot) and drives the amplified multi-scope corpus through
// the P2 concurrent replay driver (go/internal/replay/concurrentreplay), so a
// slot's fan-out actually exercises worker concurrency. Smoke and small slots
// run hermetically in the make prove common path; medium and above are
// operator-gated (ScaleSlot.Enforcement), the same hermetic/operator split
// go/internal/perfcontract already defines — no second perf contract, and the
// latency thresholds themselves are adopted from specs/scale-lab-corpus.v1.yaml,
// not redefined here.
//
// Like go/internal/ifa/saturation, this runner is a runtime scenario over
// concurrency and a durable commit seam, so it lives in a sibling subpackage
// rather than the pure, deterministic go/internal/ifa core. The hermetic path is
// credential-free: the amplified cassette is written to a temp file and driven
// into an in-memory committer that counts committed scopes and facts; no
// Postgres, graph backend, or network is touched. The committed fact total is
// invariant to worker count, which is the hermetic throughput proof — the
// amplified corpus drains completely and identically regardless of concurrency.
package throughput
