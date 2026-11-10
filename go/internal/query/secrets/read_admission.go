// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secrets

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// SecretsIAMReadAdmissionContractVersion versions the #9669
// admission-contract shape for the secrets/IAM reads: the four
// bounded list reads plus the bounded posture-summary rollup behind
// the /api/v0/secrets-iam/* routes. Canonical reads persist through
// explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned Postgres
// stores satisfy jointly, composed by the consumer. A breaking change
// to the read shape fails the frozen method-set test until that list
// is deliberately updated; bump this version alongside (manually —
// the bump is convention, the frozen list is the enforcement) so dest
// consumers can pin the shape.
const SecretsIAMReadAdmissionContractVersion = "v0"

// SecretsIAMReadAdmission is the versioned admission-contract port
// the API handler will depend on instead of the five shared handles.
// It re-declares exactly the five reads the secrets/IAM routes admit
// through — the bounded identity-trust-chain list, the bounded
// privilege-posture-observation list, the bounded secret-access-path
// list, the bounded posture-gap list, and the bounded posture-summary
// rollup — so the boundary is held honest by the compile-time mirror
// assertion below plus the frozen method-set contract test; it will
// back the eshu-api extraction handoff for this family. The seam is
// read-only on purpose: every declared method is a bounded read and
// the port carries no mutation surface, so a dest consumer
// implementing only this contract can serve secrets/IAM reads but
// cannot acquire a write it did not bargain for through it.
//
// Narrow by construction, recorded not silently assumed: the five
// methods re-declare the store signatures in this secrets/IAM-scoped
// shape instead of embedding or aliasing
// IAMIdentityTrustChainStore, IAMPrivilegePostureObservationStore,
// IAMSecretAccessPathStore, IAMPostureGapStore, or
// IAMPostureSummaryStore. This pin therefore grants nothing to the
// other families that read through shared ports — every other store
// consumer keeps its own per-route relay reason until deliberate pins
// cover them.
//
// Explicitly OUT, recorded not silently assumed (it keeps its own
// standing until a deliberate pin covers it):
//
//   - The S3 external-principal grant-posture leg
//     (GraphIAMGrantPostureStore.SummarizeS3ExternalPrincipalGrantPosture,
//     the canonical GRANTS_ACCESS_TO graph truth blended into the
//     summary response by the handler) is not pinned here: there is
//     no grant method in the frozen set, so a dest consumer cannot
//     acquire graph reads through it. The handler keeps blending the
//     graph section from its own GrantPosture handle.
//   - The grant checks, the fixed failure messages, and the
//     capability profile are handler behavior, not port shape: there
//     is no method set to freeze.
//
// Rollback: revert this pin. The seam is unwired — no handler,
// route, or caller depends on it yet — so rollback moves no traffic
// and changes no data.
type SecretsIAMReadAdmission interface {
	ListSecretsIAMIdentityTrustChains(context.Context, IAMIdentityTrustChainFilter) ([]IAMIdentityTrustChainRow, error)
	ListSecretsIAMPrivilegePostureObservations(context.Context, IAMPrivilegePostureObservationFilter) ([]IAMPrivilegePostureObservationRow, error)
	ListSecretsIAMSecretAccessPaths(context.Context, IAMSecretAccessPathFilter) ([]IAMSecretAccessPathRow, error)
	ListSecretsIAMPostureGaps(context.Context, IAMPostureGapFilter) ([]IAMPostureGapRow, error)
	SummarizeSecretsIAMPosture(context.Context, string) (IAMPostureSummary, error)
}

// The mirror assertion keeps the secrets/IAM-scoped re-declaration
// honest, and it works in one direction only: removal, rename, or
// re-signing of any of the five mirrored methods on any of the five
// stores fails compilation here. Shared-port addition deliberately
// does not widen this pin — a new store method is adopted only by
// deliberate re-declaration plus a frozen-list update. It pins no
// shared port — the assertion names the combination only as the
// shape this pin mirrors, and no other family may cite this pin as
// coverage.
var _ SecretsIAMReadAdmission = interface {
	IAMIdentityTrustChainStore
	IAMPrivilegePostureObservationStore
	IAMSecretAccessPathStore
	IAMPostureGapStore
	IAMPostureSummaryStore
}(nil)

// secretsIAMReadAdmission is the composition-only satisfier behind
// NewReadAdmissionWithReadStore: it carries the five core-owned
// Postgres stores over one guarded reader and delegates each pinned
// read to its owning store. It exists so the satellite consumes the
// pin through a single handle without a direct table or queue handle.
type secretsIAMReadAdmission struct {
	chains       PostgresIAMIdentityTrustChainStore
	observations PostgresIAMPrivilegePostureObservationStore
	paths        PostgresIAMSecretAccessPathStore
	gaps         PostgresIAMPostureGapStore
	summary      PostgresIAMPostureSummaryStore
}

// The composite assertion proves the composition-only satisfier
// carries exactly the pinned shape: any drift between the composite
// and the admission fails compilation here.
var _ SecretsIAMReadAdmission = secretsIAMReadAdmission{}

// NewReadAdmissionWithReadStore builds the secrets/IAM-scoped
// admission satisfier over a guarded read-only query port, so the
// satellite consumes the pin without a direct table or queue handle:
// pass the request-scoped db.ReadStore the same way the API wiring
// does. It serves exactly the five pinned reads over nothing wider
// than the guarded ReadStore the satellite holds.
//
// The constructor delegates to the five existing
// NewPostgresIAM*StoreWithReadStore constructors deliberately instead
// of minting parallel wiring: the transport proof (fail-closed over
// a guarded ReadStore in the wire-shapes test) covers the satellite
// path end to end, and a second construction path would be a second
// place for the guarded handle to be dropped.
func NewReadAdmissionWithReadStore(reader db.ReadStore) SecretsIAMReadAdmission {
	return secretsIAMReadAdmission{
		chains:       NewPostgresIAMIdentityTrustChainStoreWithReadStore(reader),
		observations: NewPostgresIAMPrivilegePostureObservationStoreWithReadStore(reader),
		paths:        NewPostgresIAMSecretAccessPathStoreWithReadStore(reader),
		gaps:         NewPostgresIAMPostureGapStoreWithReadStore(reader),
		summary:      NewPostgresIAMPostureSummaryStoreWithReadStore(reader),
	}
}

// ListSecretsIAMIdentityTrustChains delegates to the core-owned
// identity-trust-chain store.
func (a secretsIAMReadAdmission) ListSecretsIAMIdentityTrustChains(ctx context.Context, filter IAMIdentityTrustChainFilter) ([]IAMIdentityTrustChainRow, error) {
	return a.chains.ListSecretsIAMIdentityTrustChains(ctx, filter)
}

// ListSecretsIAMPrivilegePostureObservations delegates to the
// core-owned privilege-posture-observation store.
func (a secretsIAMReadAdmission) ListSecretsIAMPrivilegePostureObservations(ctx context.Context, filter IAMPrivilegePostureObservationFilter) ([]IAMPrivilegePostureObservationRow, error) {
	return a.observations.ListSecretsIAMPrivilegePostureObservations(ctx, filter)
}

// ListSecretsIAMSecretAccessPaths delegates to the core-owned
// secret-access-path store.
func (a secretsIAMReadAdmission) ListSecretsIAMSecretAccessPaths(ctx context.Context, filter IAMSecretAccessPathFilter) ([]IAMSecretAccessPathRow, error) {
	return a.paths.ListSecretsIAMSecretAccessPaths(ctx, filter)
}

// ListSecretsIAMPostureGaps delegates to the core-owned posture-gap
// store.
func (a secretsIAMReadAdmission) ListSecretsIAMPostureGaps(ctx context.Context, filter IAMPostureGapFilter) ([]IAMPostureGapRow, error) {
	return a.gaps.ListSecretsIAMPostureGaps(ctx, filter)
}

// SummarizeSecretsIAMPosture delegates to the core-owned
// posture-summary store. The S3 external-principal grant section is
// blended by the handler from its own graph handle, never here.
func (a secretsIAMReadAdmission) SummarizeSecretsIAMPosture(ctx context.Context, scopeID string) (IAMPostureSummary, error) {
	return a.summary.SummarizeSecretsIAMPosture(ctx, scopeID)
}
