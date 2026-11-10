// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// CodeFlowReadAdmissionContractVersion versions the #9669 W6a
// admission-contract shape for the Code-flow 4 reads (POST
// /api/v0/code/flow/cfg-summary, /pdg-summary, /reaching-def,
// /taint-path). Canonical reads persist through explicit core-owned
// admission contracts: eshu-api keeps the HTTP shape and calls this
// versioned port, which the core-owned Postgres code-flow store
// satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const CodeFlowReadAdmissionContractVersion = "v0"

// CodeFlowReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of a direct CodeFlowStore
// handle. It re-declares exactly the one read the Code-flow 4 routes
// admit through — the bounded cumulative active code-flow lookup —
// so the boundary is held honest by the compile-time mirror assertion
// below plus the frozen method-set contract test; it will back the
// eshu-api extraction handoff for this family. The seam is read-only
// on purpose: the declared method is a bounded read and the port
// carries no mutation surface, so a dest consumer implementing only
// this contract can serve Code-flow 4 reads but cannot acquire a
// write it did not bargain for through it.
//
// W-wave consumer: W6a Code-flow 4 ONLY (per the arbiter W-wave
// ruling on eshu-hq/eshu#9669, APPROVED-WITH-CONDITIONS). The
// satellite records this pin in a follow-up; a satellite PR
// consuming an unminted pin is invalid. Arbiter review of this pin
// precedes satellite termination of the Code-flow 4 legs. W6b-W6f
// are NOT approved and are not pre-minted here.
//
// In scope (each route admits its read through exactly this):
//
//   - POST /api/v0/code/flow/cfg-summary admits its bounded parser
//     dataflow read through ListCodeFlow with Kind cfg_summary.
//   - POST /api/v0/code/flow/pdg-summary admits its bounded parser
//     dataflow read through ListCodeFlow with Kind pdg_summary.
//   - POST /api/v0/code/flow/reaching-def admits its bounded parser
//     dataflow read through ListCodeFlow with Kind reaching_def.
//   - POST /api/v0/code/flow/taint-path admits its reducer-owned
//     taint-evidence read through ListCodeFlow with Kind taint_path.
//
// Narrow by construction, recorded not silently assumed: the method
// re-declares the CodeFlowStore signature in this Code-flow-scoped
// shape instead of embedding the CodeFlowStore port or aliasing the
// store, so no other family can cite this pin — a blanket or
// shared-port pin is forbidden, and the widened shape needs arbiter
// review before satellite consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - The grant checks (codeGrantAccessFilter plus the scoped
//     repository selector), the unsupported-language short-circuit,
//     the fixed failure messages ("code-flow read failed" and
//     "code-flow store is unavailable"), and QueryProfile are handler
//     behavior, not port shape: there is no method set to freeze,
//     the same standing as Clock and FinalizeTimeout in the mutation
//     pins.
//   - No search, list, ledger, stored-summary, or snapshot surface
//     exists on this family; the frozen single-method set holds that
//     exclusion the same way.
//
// Rollback: revert this pin. The seam is unwired — no handler,
// route, or caller depends on it yet — so rollback moves no traffic
// and changes no data.
type CodeFlowReadAdmission interface {
	ListCodeFlow(context.Context, codemodel.CodeFlowFilter) (codemodel.CodeFlowReadModel, error)
}

// The mirror assertion keeps the Code-flow-scoped re-declaration
// honest, and it works in one direction only: removal, rename, or
// re-signing of the mirrored method on the staying handler port
// fails compilation here. It names the staying handler-facing port
// (not the concrete Postgres store) as the mirrored shape on
// purpose: the pin must stay identical to what the handlers
// actually call, while the concrete store keeps its own standing
// for storage evolution. Shared-port addition deliberately does not
// widen this pin — a new handler-port method is adopted only by
// deliberate re-declaration plus a frozen-list update. It pins no
// shared port — the assertion names CodeFlowStore only as the shape
// this pin mirrors, and no other family may cite this pin as
// coverage.
var _ CodeFlowReadAdmission = CodeFlowStore(nil)

// NewCodeFlowReadAdmissionWithReadStore builds the Code-flow-scoped
// admission satisfier over a guarded read-only query port, so the
// satellite consumes the pin without a direct table or queue handle:
// pass the request-scoped db.Queryer the same way the API wiring
// does. It serves exactly the one pinned read over nothing wider
// than the guarded Queryer the satellite holds.
//
// The constructor delegates to the existing
// codemodel.NewPostgresCodeFlowStoreWithReadStore deliberately
// instead of minting parallel wiring: the transport proof
// (fail-closed over a guarded Queryer plus the full wire-object byte
// comparison against the direct constructor in the wire-shapes test)
// covers the satellite path end to end, and a second construction
// path would be a second place for the guarded handle to be
// dropped.
func NewCodeFlowReadAdmissionWithReadStore(reader db.Queryer) CodeFlowReadAdmission {
	return codemodel.NewPostgresCodeFlowStoreWithReadStore(reader)
}
