// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package firstrunbench scores a captured `eshu first-run --json` envelope
// against the first-five-minutes onboarding success criteria from issue #1772.
// It holds the logic behind the `eshu first-run-benchmark` command: decoding
// the envelope (ParseEnvelope, ReadEnvelope), grading it (Evaluate over
// Measurements into a Verdict of Criterion rows), and rendering the
// operator-facing scorecard (RenderVerdict, Marker).
//
// The load-bearing invariant is health-only rejection: Evaluate fails the
// benchmark whenever a "first answer" rests on process health or readiness
// alone — a missing query answer, missing truth metadata, missing source
// handle, incomplete indexing, or an error envelope each fail a required
// criterion. Optional criteria (time to answer, manual steps) record honest
// not-measured gaps and never flip an otherwise-complete run to fail.
//
// Envelope, Result, Step, and Diagnostic mirror the wire shape the first-run
// command emits from go/cmd/eshu (package main, which this package cannot
// import). Every field keeps the emitter's JSON tag and an equivalent type so
// a corrupt artifact fails the decode instead of being silently scored; the
// wire-parity test in go/cmd/eshu/first_run_benchmark_cmd_test.go pins the
// two shapes together. The demo-benchmark family in go/cmd/eshu reuses the
// criterion vocabulary, EnvelopeError, ReadEnvelope, and Marker for its own
// scorecard, so those stay exported.
//
// Flag resolution, stdin/stdout stream selection, and exit-code mapping stay
// in the cobra wrapper go/cmd/eshu/first_run_benchmark_cmd.go.
package firstrunbench
