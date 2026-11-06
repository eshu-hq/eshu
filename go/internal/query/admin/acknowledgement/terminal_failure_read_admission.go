// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acknowledgement

import (
	"context"

	pgstorage "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TerminalFailureReadAdmissionContractVersion versions the #9669 D1
// admission-contract shape for the operator terminal-failure reads
// (the admin-only acknowledgement list and the per-instance failure
// discovery list). Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and
// calls this versioned port, which the core-owned Postgres store
// satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen list
// is the enforcement) so dest consumers can pin the shape.
const TerminalFailureReadAdmissionContractVersion = "v0"

// TerminalFailureReader is the read-only subset of Acknowledger behind
// the terminal-failure reads: it lists recorded acknowledgements and
// discovers unresolved failures without resolving, recording, or
// claiming anything. The acknowledgement mutation surface
// (ResolveTarget, Acknowledge) stays on TerminalFailureAdmission; a
// dest consumer implementing only this contract can serve the reads but
// cannot mark a failure known and expected through it.
type TerminalFailureReader interface {
	ListTerminalFailureAcks(ctx context.Context, filter pgstorage.TerminalFailureAckListFilter) ([]pgstorage.TerminalFailureAckRecord, error)
	ListTerminalFailures(ctx context.Context, filter pgstorage.TerminalFailureListFilter) (pgstorage.TerminalFailureListPage, error)
}

// TerminalFailureReadAdmission is the versioned admission-contract port
// the API handler depends on instead of a direct table handle. It
// aliases the narrow failure reader so the boundary is
// identity-verified by the frozen contract test rather than a parallel
// hand-maintained shape; it will back the eshu-api extraction handoff
// for this family.
//
// D-wave consumer: D3 (admin/identity/recovery reads, per the arbiter
// D-wave ruling on #9669). The satellite records this pin in a
// follow-up; a satellite PR consuming an unminted pin is invalid.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type TerminalFailureReadAdmission = TerminalFailureReader

var _ TerminalFailureReadAdmission = (*pgstorage.TerminalFailureAckStore)(nil)
