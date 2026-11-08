// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auth

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
)

// AuditAppendAdmissionContractVersion versions the #9669 O2
// admission-contract shape for the audit-append port behind the 18
// tier-C audit-emitting routes. The unversioned
// GovernanceAuditAppender in this package carries no version constant
// and no frozen method-set test, so dest reimplementation against it
// risks mis-stamped, dropped, or forged events; this pin freezes the
// one method every emitter admits through. A breaking change to the
// append shape fails the frozen method-set test until that list is
// deliberately updated; bump this version alongside (manually — the
// bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
const AuditAppendAdmissionContractVersion = "v0"

// AuditAppendAdmission is the versioned admission-contract port the API
// handlers will depend on instead of the shared unversioned handle. It
// re-declares exactly the one method the 18 tier-C audit-emitting
// routes admit through — Append, mirroring GovernanceAuditAppender —
// so the boundary is held honest by the compile-time mirror assertion
// below plus the frozen method-set contract test. One shared pin
// covers all 18 routes (O2 ORDER).
//
// Narrow by construction, recorded not silently assumed: the method
// re-declares the shared-port signature in this audit-scoped shape
// instead of embedding or aliasing GovernanceAuditAppender. This pin
// therefore grants nothing to any other family that appends through
// the shared port — every non-audit consumer keeps its own standing
// until a deliberate pin covers it.
//
// D-wave consumer: satellite records this pin in api/version.go and
// contracts/ with per-route byte-parity/conformance plus D0 re-review;
// until mint + arbiter review + record, all 18 routes carry tier-C
// dispatch. A satellite PR consuming an unminted pin is invalid.
// Arbiter review of this pin precedes satellite termination of any of
// the 18 legs.
//
// Rollback: revert this pin. The seam is unwired — no handler, route,
// or caller depends on it yet — so rollback moves no traffic and
// changes no data.
type AuditAppendAdmission interface {
	Append(context.Context, []governanceaudit.Event) error
}

// The mirror assertion keeps the audit-scoped re-declaration honest,
// and it works in one direction only: removal, rename, or re-signing
// of Append on the shared port fails compilation here. Shared-port
// addition deliberately does not widen this pin — a new
// GovernanceAuditAppender method is adopted only by deliberate
// re-declaration plus a frozen-list update. It pins no other port —
// the assertion names GovernanceAuditAppender only as the shape this
// pin mirrors, and no other family may cite this pin as coverage.
var _ AuditAppendAdmission = GovernanceAuditAppender(nil)
