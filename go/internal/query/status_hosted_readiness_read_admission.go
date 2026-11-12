// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// StatusHostedReadinessReadAdmissionContractVersion versions the #9669
// admission-contract shape for the Status hosted-readiness graph legs: the
// repository-count read and the workload-leftovers detection read behind GET
// /api/v0/status/hosted-readiness. Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and calls
// this versioned port, which the core-owned graph reader satisfies. A
// breaking change to the read shape fails the frozen method-set test until
// that list is deliberately updated; bump this version alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
const StatusHostedReadinessReadAdmissionContractVersion = "v0"

// StatusHostedReadinessReadAdmission is the versioned admission-contract port
// the API handler will depend on instead of a direct graph handle. It
// re-declares exactly the two graph reads the hosted-readiness route admits
// through — the repository-count leg (RunSingle over
// "MATCH (r:Repository) RETURN count(r) as count") and the
// workload-leftovers detection leg (Run through legacyworkload.Detect) — so
// the boundary is held honest by the compile-time mirror assertion below
// plus the frozen method-set contract test; it will back the eshu-api
// extraction handoff for these legs. The seam is read-only on purpose: both
// declared methods are bounded graph reads and the port carries no mutation,
// session, or transaction surface, so a dest consumer implementing only this
// contract can serve the hosted-readiness graph legs but cannot acquire a
// graph write it did not bargain for through it.
//
// D-wave consumer: Status mints follow-up, per the arbiter
// APPROVED-WITH-CONDITIONS ruling on #9669. The satellite records this pin
// in a follow-up; a satellite PR consuming an unminted pin is invalid.
// Arbiter review of this pin precedes satellite termination of the
// hosted-readiness graph legs.
//
// Narrow by construction, recorded not silently assumed: the two methods
// re-declare the GraphQuery signatures in this Status-scoped shape instead
// of embedding the shared GraphQuery port, so no other family can cite this
// pin — pinning the shared port here would grant every family that reads
// through it, a blanket pin the arbiter ruling forbids, and the widened
// shape needs arbiter review before satellite consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - The status.Reader snapshot leg on the hosted-readiness route stays
//     covered by StatusReadAdmission v0, not widened here.
//   - The GraphBackend selector is config, not a port: there is no method
//     set to freeze.
//   - The Detect algorithm (candidate paging, examined caps, twin
//     classification) is callee behavior, not port shape: this pin freezes
//     only the Run/RunSingle transport the detection reads through.
//   - The answer-narration posture override (NarrationPosture func field)
//     is a handler field, not a port: there is no method set to freeze, the
//     same standing as Clock/FinalizeTimeout in the mutation pins.
//   - No suppression leg exists on this route; the suppressions mutation
//     row stays the Decision 2 survivor, never a read termination here.
//
// This pin is not combined with the governance Summary pin: each admits only
// its own legs, and a dest consumer takes exactly the legs it serves.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type StatusHostedReadinessReadAdmission interface {
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
	RunSingle(context.Context, string, map[string]any) (map[string]any, error)
}

// The mirror assertion keeps the Status-scoped re-declaration honest: any
// one-sided signature drift on the two graph reads fails compilation here.
// It pins no wider graph surface — the assertion names querycontract.GraphQuery
// only as the shape this pin mirrors, and no other family may cite this pin
// as coverage. One-directional on purpose: the pin never asserts back onto
// the shared port.
var _ StatusHostedReadinessReadAdmission = querycontract.GraphQuery(nil)
