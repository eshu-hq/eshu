// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
)

// SetupReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the setup-state read (GET
// /api/v0/auth/setup-state). Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and
// calls this versioned port, which the core-owned setup store
// satisfies. A breaking change to the read shape fails the frozen
// method-set test until that list is deliberately updated; bump this
// version alongside (manually — the bump is convention, the frozen
// list is the enforcement) so dest consumers can pin the shape.
const SetupReadAdmissionContractVersion = "v0"

// SetupReadAdmission is the versioned admission-contract port the API
// handler will depend on instead of the full setup store. It declares
// exactly the one read the setup-state route admits through —
// SetupNeeded, mirroring SetupStore as exercised through the
// setup-state leg — so the boundary is held honest by the
// compile-time mirror assertion below plus the frozen method-set
// contract test. It will back the eshu-api extraction handoff for
// this read. The seam is read-only on purpose: the declared method is
// a bounded read and the port carries no mutation surface, so a dest
// consumer implementing only this contract can serve the setup-state
// read but cannot acquire a write it did not bargain for through it.
//
// W-wave consumer: W5 setup read half (GET /api/v0/auth/setup-state
// only; the three setup mutations ride the sibling SetupAdmission
// writer pin in setup_types.go, never this one).
//
// Narrow by construction, recorded not silently assumed: the method
// re-declares the store signature in this setup-read-scoped shape
// instead of embedding or aliasing SetupStore. This pin therefore
// grants nothing to the other setup consumers — the claim, admin,
// MFA, and credential-verification legs keep their own writer-pin
// standing until deliberate pins cover them.
type SetupReadAdmission interface {
	SetupNeeded(ctx context.Context) (bool, error)
}

// The mirror assertion keeps the setup-read-scoped re-declaration
// honest, and it works in one direction only: removal, rename, or
// re-signing of SetupNeeded on the staying handler-facing port fails
// compilation here. A new method on that port does not widen this pin
// — adoption takes deliberate re-declaration plus a frozen-list
// update. It pins no shared port — the assertion names the read
// surface only as the shape this pin mirrors, and no other family may
// cite this pin as coverage.
var _ SetupReadAdmission = SetupStore(nil)
