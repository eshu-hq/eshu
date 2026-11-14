// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
)

// CodeHardcodedSecretReadAdmissionContractVersion versions the #9669
// admission-contract shape for the W6c secret read (POST
// /api/v0/code/security/secrets/investigate). Canonical reads persist
// through explicit core-owned admission contracts: eshu-api keeps the
// HTTP shape and calls this versioned port, which the core-owned
// Postgres content store satisfies. A breaking change to the read shape
// fails the frozen method-set test until that list is deliberately
// updated; bump this version alongside (manually — the bump is
// convention, the frozen list is the enforcement) so dest consumers can
// pin the shape.
const CodeHardcodedSecretReadAdmissionContractVersion = "v0"

// CodeHardcodedSecretReadAdmission is the versioned admission-contract
// port the API handler will depend on instead of the
// HardcodedSecretInvestigator assertion plus the optional
// HardcodedSecretSourceInvestigator upgrade. It re-declares exactly the
// two reads the W6c secret route admits through — the plain
// investigation (InvestigateHardcodedSecrets) and the
// storage-path-reporting twin (InvestigateHardcodedSecretsWithSource) —
// so the boundary is held honest by the compile-time mirror assertion
// below plus the frozen method-set contract test; it will back the
// eshu-api extraction handoff for this family. The seam is read-only on
// purpose: both declared methods are bounded content reads and the port
// carries no mutation surface, so a dest consumer implementing only this
// contract can serve secret reads but cannot acquire a write it did not
// bargain for through it.
//
// W-wave consumer: W6c secret ONLY (per the arbiter supplemental ruling
// on eshu-hq/eshu-api#98, §2C). The pin stays isolated from the
// content-investigation pin on purpose: redacted secret-line text takes
// the least-privilege seal, never a bundle. The satellite records this
// pin in a follow-up; a satellite PR consuming an unminted pin is
// invalid. Arbiter review of this pin precedes satellite termination of
// the secret leg. Never cite SecretsIAMReadAdmission here: it covers
// /api/v0/secrets-iam/* only, not this route.
//
// In scope: the one W6c secret read. The route prefers the WithSource
// twin and tolerates the plain-only store (side-table-source default),
// against the shared Postgres codeContentGrantScope grant binding:
//
//   - POST /api/v0/code/security/secrets/investigate
//
// Both methods are IN because production satisfies both: the mirror
// below compiles against the production content reader for the pair,
// proven at mint time. Had production satisfied only the plain
// interface, the pin would have minted 1-method with the side-table
// default recorded — not the reverse.
//
// Narrow by construction, recorded not silently assumed: the two methods
// re-declare the staying-port signatures in this secret-scoped shape
// instead of embedding either source port as the exported surface or
// aliasing either port, so no other family can cite this pin — a blanket
// or shared-port pin is forbidden, and the widened shape needs arbiter
// review before satellite consumption.
//
// Explicitly OUT, recorded not silently assumed (each keeps its own
// standing until a deliberate pin covers it):
//
//   - Topic and structural-inventory investigation (sibling pin, §2B).
//   - Run, RunSingle, and every other graph method: this pin is
//     Postgres-content only; secret investigation admits no graph call.
//   - Every other content read (search, list, file, entity, topic):
//     a new content method is adopted only by deliberate re-declaration
//     plus a frozen-list update.
//   - The grant checks (codeContentGrantScope plus the repository
//     selector), the source-label response shaping, the fixed failure
//     messages, and QueryProfile are handler behavior, not port shape:
//     there is no method set to freeze.
//   - No constructor, selector, or ReadStore surface: the satellite
//     satisfies this port with its own wiring; this pin carries the read
//     shape only.
//
// Rollback: revert this pin. The seam is unwired — no handler, route, or
// caller depends on it yet — so rollback moves no traffic and changes no
// data.
type CodeHardcodedSecretReadAdmission interface {
	InvestigateHardcodedSecrets(context.Context, HardcodedSecretInvestigationRequest) ([]HardcodedSecretFindingRow, error)
	InvestigateHardcodedSecretsWithSource(context.Context, HardcodedSecretInvestigationRequest) ([]HardcodedSecretFindingRow, HardcodedSecretReadSource, error)
}

// codeHardcodedSecretReadAdmissionMirror keeps the secret re-declaration
// honest about both of its sources in one assertion: it embeds the two
// staying handler-facing ports, so the promoted method set is their
// union, and the assignment below holds exactly while that union still
// serves this pin. Removal, rename, or re-signing of either of the two
// mirrored methods on either source port fails compilation here —
// including on the production *ContentReader, which satisfies both and
// settles the §2C 1-vs-2 mint condition at two. It works in one direction
// only: a new method on either source port does not widen this pin —
// adoption takes deliberate re-declaration plus a frozen-list update. It
// pins no shared port — the assertion names
// HardcodedSecretInvestigator and HardcodedSecretSourceInvestigator only
// as the shapes this pin mirrors, and no other family may cite this pin
// as coverage.
type codeHardcodedSecretReadAdmissionMirror struct {
	HardcodedSecretInvestigator
	HardcodedSecretSourceInvestigator
}

var _ CodeHardcodedSecretReadAdmission = (*codeHardcodedSecretReadAdmissionMirror)(nil)
