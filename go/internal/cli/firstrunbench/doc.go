// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package firstrunbench scores a captured `eshu first-run --json` envelope
// against the first-five-minutes onboarding success criteria from issue #1772.
// It holds the logic behind the `eshu first-run-benchmark` command: fetching
// the artifact bytes (ReadEnvelope), grading the decoded envelope (Evaluate
// over Measurements into a Verdict of Criterion rows), and rendering the
// operator-facing scorecard (RenderVerdict, Marker).
//
// The load-bearing invariant is health-only rejection: Evaluate fails the
// benchmark whenever a "first answer" rests on process health or readiness
// alone — a missing query answer, missing truth metadata, missing source
// handle, incomplete indexing, or an error envelope each fail a required
// criterion. Optional criteria (time to answer, manual steps) record honest
// not-measured gaps and never flip an otherwise-complete run to fail.
//
// The envelope contract itself lives in internal/cli/firstrun: Evaluate
// consumes a firstrun.Envelope decoded by firstrun.ParseEnvelope, so the
// benchmark, the evidence report, and the emitter share one shape and a
// corrupt artifact fails the decode instead of being silently scored. The
// demo-benchmark family (go/internal/cli/demo and its go/cmd/eshu wrapper)
// imports the criterion vocabulary, ReadEnvelope, and Marker from here for its
// own scorecard, so those stay exported; it types its envelope error as
// firstrun.EnvelopeError, which belongs to that package, not this one.
//
// Flag resolution, stdin/stdout stream selection, and exit-code mapping stay
// in the cobra wrapper go/cmd/eshu/first_run.go.
package firstrunbench
