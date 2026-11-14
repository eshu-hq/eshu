// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
)

// CodeContentInvestigationReadAdmissionContractVersion versions the #9669
// admission-contract shape for the W6c content investigation reads (POST
// /api/v0/code/structure/inventory and POST
// /api/v0/code/topics/investigate). Canonical reads persist through
// explicit core-owned admission contracts: eshu-api keeps the HTTP shape
// and calls this versioned port, which the core-owned Postgres content
// store satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen list
// is the enforcement) so dest consumers can pin the shape.
const CodeContentInvestigationReadAdmissionContractVersion = "v0"

// CodeContentInvestigationReadAdmission is the versioned
// admission-contract port the API handler will depend on instead of the
// unexported structuralInventoryContentStore handle plus the
// CodeTopicContentInvestigator assertion. It re-declares exactly the
// three reads the W6c 2 admit through — the structural-inventory content
// reads (InspectStructuralInventory, CountStructuralInventoryByFile) and
// the topic investigation (InvestigateCodeTopic) — so the boundary is
// held honest by the compile-time mirror assertion below plus the frozen
// method-set contract test; it will back the eshu-api extraction handoff
// for this family. The seam is read-only on purpose: every declared
// method is a bounded content read and the port carries no mutation
// surface, so a dest consumer implementing only this contract can serve
// investigation reads but cannot acquire a write it did not bargain for
// through it.
//
// W-wave consumer: W6c content-investigation ONLY (per the arbiter
// supplemental ruling on eshu-hq/eshu-api#98, §2B). The satellite records
// this pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid. Arbiter review of this pin precedes satellite termination of
// the W6c investigation legs.
//
// In scope: the W6c 2 content investigation reads. Each route admits its
// reads through exactly this pin, against the shared Postgres
// codeContentGrantScope grant binding:
//
//   - POST /api/v0/code/structure/inventory (InspectStructuralInventory
//     plus CountStructuralInventoryByFile for the function_count_by_file
//     kind; the satellite port must preserve the empty-grant blocked
//     page and the allocated empty results shape exactly)
//   - POST /api/v0/code/topics/investigate (InvestigateCodeTopic; the
//     satellite port must preserve the ErrCodeTopicDeadlineTruncated
//     deadline-partial semantics exactly — a deadline carries its bounded
//     partial rows alongside the marker error, every other failure drops
//     the rows)
//
// Narrow by construction, recorded not silently assumed: the three
// methods re-declare the staying-port signatures in this
// investigation-scoped shape instead of embedding either source port as
// the exported surface or aliasing either port, so no other family can
// cite this pin — a blanket or shared-port pin is forbidden, and the
// widened shape needs arbiter review before satellite consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - Hardcoded-secret investigation (isolated least-privilege pin, §2C):
//     redacted secret-line text is never bundled here.
//   - Run, RunSingle, and every other graph method: this pin is
//     Postgres-content only; content-investigation admits no graph call.
//   - Every other content read (search, list, file, entity, secrets):
//     a new content method is adopted only by deliberate re-declaration
//     plus a frozen-list update.
//   - The grant checks (codeContentGrantScope plus the repository
//     selector), the fixed failure messages, and QueryProfile are handler
//     behavior, not port shape: there is no method set to freeze.
//   - No constructor, selector, or ReadStore surface: the satellite
//     satisfies this port with its own wiring; this pin carries the read
//     shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type CodeContentInvestigationReadAdmission interface {
	InspectStructuralInventory(context.Context, StructuralInventoryRequest) ([]EntityContent, error)
	CountStructuralInventoryByFile(context.Context, StructuralInventoryRequest) ([]StructuralInventoryFileCount, error)
	InvestigateCodeTopic(context.Context, CodeTopicInvestigationRequest) ([]CodeTopicEvidenceRow, error)
}

// codeContentInvestigationReadAdmissionMirror keeps the investigation
// re-declaration honest about both of its sources in one assertion: it
// embeds the two staying handler-facing ports, so the promoted method
// set is their union, and the assignment below holds exactly while that
// union still serves this pin. Removal, rename, or re-signing of any of
// the three mirrored methods on either source port fails compilation
// here. It works in one direction only: a new method on either source
// port does not widen this pin — adoption takes deliberate re-declaration
// plus a frozen-list update. It pins no shared port — the assertion
// names structuralInventoryContentStore and CodeTopicContentInvestigator
// only as the shapes this pin mirrors, and no other family may cite this
// pin as coverage.
type codeContentInvestigationReadAdmissionMirror struct {
	structuralInventoryContentStore
	CodeTopicContentInvestigator
}

var _ CodeContentInvestigationReadAdmission = (*codeContentInvestigationReadAdmissionMirror)(nil)
