// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/local"
)

// ProfileReadAdmissionContractVersion versions the #9669 W5
// admission-contract shape for the profile read (GET
// /api/v0/auth/profile). Canonical reads persist through explicit
// core-owned admission contracts: eshu-api keeps the HTTP shape and
// calls this versioned port, which the core-owned local-identity
// profile store satisfies. A breaking change to the read shape fails
// the frozen method-set test until that list is deliberately updated;
// bump this version alongside (manually — the bump is convention, the
// frozen list is the enforcement) so dest consumers can pin the shape.
const ProfileReadAdmissionContractVersion = "v0"

// ProfileReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of the full profile lister. It
// declares exactly the one read the profile route admits through —
// GetLocalIdentityMFAStatus, mirroring local.IdentityProfileLister as
// exercised through the profile leg — so the boundary is held honest
// by the compile-time mirror assertion below plus the frozen
// method-set contract test. It will back the eshu-api extraction
// handoff for this read. The seam is read-only on purpose: the
// declared method is a bounded read and the port carries no mutation
// surface, so a dest consumer implementing only this contract can
// serve the profile read but cannot acquire a write it did not
// bargain for through it.
//
// W-wave consumer: W5 profile read (GET /api/v0/auth/profile only;
// the token-list, user-ID resolution, and TOTP enrollment legs ride
// the sibling LocalIdentityAdmission pin, never this one).
//
// Narrow by construction, recorded not silently assumed: the method
// re-declares the store signature in this profile-read-scoped shape
// instead of embedding or aliasing local.IdentityProfileLister. This
// pin therefore grants nothing to the other local-identity consumers —
// every other lister consumer keeps its own per-route relay reason
// until deliberate pins cover them.
type ProfileReadAdmission interface {
	GetLocalIdentityMFAStatus(ctx context.Context, subjectIDHash string, asOf time.Time) (local.IdentityMFAStatus, error)
}

// The mirror assertion keeps the profile-read-scoped re-declaration
// honest, and it works in one direction only: removal, rename, or
// re-signing of GetLocalIdentityMFAStatus on the staying
// handler-facing port fails compilation here. A new method on that
// port does not widen this pin — adoption takes deliberate
// re-declaration plus a frozen-list update. It pins no shared port —
// the assertion names the read surface only as the shape this pin
// mirrors, and no other family may cite this pin as coverage.
var _ ProfileReadAdmission = local.IdentityProfileLister(nil)
