// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auth

import (
	"context"
)

// SignInPolicyReadAdmissionContractVersion versions the #9669 O6
// admission-contract shape for the sign-in-policy reads (GET
// /api/v0/auth/admin/sign-in-policy, GET
// /api/v0/auth/sign-in-policy, GET /api/v0/auth/providers).
// Canonical reads persist through explicit core-owned admission
// contracts: eshu-api keeps the HTTP shape and calls this versioned
// port, which the core-owned sign-in-policy store satisfies. A
// breaking change to the read shape fails the frozen method-set test
// until that list is deliberately updated; bump this version
// alongside (manually — the bump is convention, the frozen list is
// the enforcement) so dest consumers can pin the shape.
const SignInPolicyReadAdmissionContractVersion = "v0"

// SignInPolicyReadAdmission is the versioned admission-contract port
// the API handler will depend on instead of the shared read handle.
// It declares exactly the one read the sign-in-policy legs admit
// through — GetSignInPolicy, mirroring SignInPolicyReadStore — so
// the boundary is held honest by the compile-time mirror assertion
// below plus the frozen method-set contract test. It will back the
// eshu-api extraction handoff for these reads. The seam is read-only
// on purpose: the declared method is a bounded read and the port
// carries no mutation surface, so a dest consumer implementing only
// this contract can serve the policy read but cannot acquire a write
// it did not bargain for through it.
//
// D-wave consumer: O6 pin 1 (per the arbiter O6 ruling mint order).
// It unblocks the admin sign-in-policy GET on shared-key after
// record plus parity plus D0 review; the public sign-in-policy GET
// and the providers GET stay gated on the O3 posture election, and a
// satellite PR consuming an unminted pin is invalid. Arbiter review
// of this pin precedes satellite termination of the sign-in-policy
// read legs.
//
// Narrow by construction, recorded not silently assumed: the method
// re-declares the shared-port signature in this sign-in-policy-scoped
// shape instead of embedding or aliasing SignInPolicyReadStore. This
// pin therefore grants nothing to the other families that read
// through shared ports — every other SignInPolicyReadStore consumer
// keeps its own per-route relay reason until deliberate pins cover
// them.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - The sign-in-policy mutation (UpsertSignInPolicy) stays on the
//     root SignInPolicyMutationStore / SignInPolicyAdmission pin,
//     which this file does not touch: there is no write method set
//     to freeze here.
//   - Grant checks, fail-open read conventions, and fixed failure
//     messages are handler behavior, not port shape: there is no
//     method set to freeze.
//
// Rollback: revert this pin. The seam is unwired — no handler,
// route, or caller depends on it yet — so rollback moves no traffic
// and changes no data.
type SignInPolicyReadAdmission interface {
	GetSignInPolicy(ctx context.Context, tenantID string) (SignInPolicy, error)
}

// The mirror assertion keeps the sign-in-policy-scoped re-declaration
// honest, and it works in one direction only: removal, rename, or
// re-signing of the mirrored method on the shared port fails
// compilation here. Shared-port addition deliberately does not widen
// this pin — a new shared-port method is adopted only by deliberate
// re-declaration plus a frozen-list update. It pins no shared port —
// the assertion names the read surface only as the shape this pin
// mirrors, and no other family may cite this pin as coverage.
var _ SignInPolicyReadAdmission = SignInPolicyReadStore(nil)
