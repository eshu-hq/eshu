// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// SSMHostInventoryReadAdmissionContractVersion versions the #9669 W8b
// admission-contract shape for the SSM host inventory read (GET
// /api/v0/cloud/ssm-host-inventory). Canonical reads persist through
// explicit core-owned admission contracts: eshu-api keeps the HTTP shape and
// calls this versioned port, which the core-owned content reader and graph
// reader satisfy jointly. A breaking change to the read shape fails the
// frozen method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
const SSMHostInventoryReadAdmissionContractVersion = "v0"

// SSMHostInventoryReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of the facts-store and GraphQuery
// handles directly. It re-declares exactly the two reads the SSM route
// admits through — the admitted-facts read through SSMHostActiveFacts plus
// the bounded graph read through Run (2 seed reads plus 2 node reads,
// skipped when Graph is nil or the page is empty) — so the boundary is held
// honest by the compile-time witness assertion below plus the frozen
// method-set contract test; it will back the eshu-api extraction handoff for
// this family. The seam is read-only on purpose: every declared method is a
// bounded read and the port carries no mutation surface, so a dest consumer
// implementing only this contract can serve SSM reads but cannot acquire a
// write it did not bargain for through it. It lives in package query because
// the filter and fact types are unexported.
//
// Satellite consumer: the W8b-wide2 leaf, per the arbiter
// SUPPLEMENTAL-PIN-OWED ruling on eshu-hq/eshu-api#101. The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid. Arbiter review of this pin precedes satellite termination of the
// SSM leg.
//
// In scope: the SSM host inventory read. The route admits its reads through
// exactly this pin:
//
//   - GET /api/v0/cloud/ssm-host-inventory
//
// Narrow by construction, recorded not silently assumed: the two methods
// re-declare the staying-port signatures in this SSM-scoped shape instead of
// embedding GraphQuery or aliasing the stores, so no other family can cite
// this pin — a blanket or shared-port pin is forbidden, and the widened
// shape needs arbiter review before satellite consumption. The drift, IaC,
// TerraformDrift, CodeGraph, CodeRelationships, SemanticSearch, and
// SupplyChain pins are not cited as coverage here: each family stands on its
// own re-declaration, and this pin adopts only the reads named above.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own standing
// until a deliberate pin covers it):
//
//   - GraphQuery.RunSingle stays unpinned here by construction: the SSM lane
//     never issues a single-row graph read.
//   - ssmhost.Bounds validation and Now are config/clock, not port shape;
//     the zero-bounds 503 is handler behavior the parity suite must drive.
//   - The grant checks, the fixed failure messages, and QueryProfile are
//     handler behavior, not port shape: there is no method set to freeze.
//   - No constructor, selector, or ReadStore surface: the satellite
//     satisfies this port with its own wiring; this pin carries the read
//     shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type SSMHostInventoryReadAdmission interface {
	SSMHostActiveFacts(context.Context, ssmHostInventoryFilter) ([]ssmAdmittedFact, error)
	Run(context.Context, string, map[string]any) ([]map[string]any, error)
}

// ssmHostInventoryReadAdmissionMirror is the unexported mirror that keeps the
// SSM-scoped re-declaration honest about both of its staying handler-facing
// halves in one assertion (the DeadCode witness-struct precedent): the graph
// half mirrors GraphQuery and the facts half mirrors *ContentReader, so a
// one-sided signature drift on either pinned read fails compilation here.
// The assignment below proves the (GraphQuery + *ContentReader) pair the
// handler runs against today satisfies the pin; it pins no wider store
// surface, and no other family may cite this pin as coverage.
type ssmHostInventoryReadAdmissionMirror struct {
	querycontract.GraphQuery
	*ContentReader
}

var _ SSMHostInventoryReadAdmission = ssmHostInventoryReadAdmissionMirror{}
