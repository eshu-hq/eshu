// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

// ReindexProgressReadAdmissionContractVersion versions the #9669 Gap
// 02 admission-contract shape for the reindex progress read (GET
// /api/v0/admin/reindex/progress). Canonical reads persist through
// explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned wiring
// satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const ReindexProgressReadAdmissionContractVersion = "v0"

// ReindexProgressReadAdmission is the versioned admission-contract
// port the API handler depends on instead of a direct reader handle.
// It aliases the narrow progress reader so the boundary is
// identity-verified by the frozen contract test rather than a parallel
// hand-maintained shape; it will back the eshu-api extraction handoff
// for this read. The seam is read-only on purpose: the aliased
// interface carries the fleet watermark, per-repository watermarks,
// and per-scope newest-activated-full reads, and the endpoint records
// nothing, claims nothing, and completes nothing through it — so a
// dest consumer implementing only this contract can serve the progress
// report but cannot acquire a write it did not bargain for through it.
//
// D-wave consumer: D3 (admin reads, per the arbiter D-wave ruling on
// #9669: Decision 3 / census Q4 — admin reads go native only after the
// pin). The satellite records this pin in a follow-up; a satellite PR
// consuming an unminted pin is invalid.
//
// Explicitly OUT, recorded not silently assumed: the reindex mutation
// ports stay out (the D7 ReindexAdmission already pins POST
// /api/v0/admin/reindex through its own narrow ports, and names this
// progress read as out of its scope). The mutation and the progress
// view meet only at the watermark rows, never through a shared pin;
// pinning them together would be a blanket pin.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type ReindexProgressReadAdmission = ReindexProgressReader
