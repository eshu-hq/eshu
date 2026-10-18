// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	// RefOverlayBranchObservationFactKind identifies one tracked
	// non-default ref observation: a deployment-referenced branch, tag, or
	// commit a later Phase 1 leaf observes, carrying the repo, the ref, its
	// kind, its resolvability, and the observed head SHA when resolvable.
	// Phase 1 of issue #5393 defines this contract only; the ref-extraction
	// emitter and the reducer/query consumption land in sibling Phase 1
	// leaves of the same epic. Until then no correlation consumer reads
	// this kind, so branch observations produce zero canonical rows.
	RefOverlayBranchObservationFactKind = "ref_overlay.branch_observation"

	// ConfigKeyDivergenceFactKind identifies one per-environment config-key
	// divergence row: a config key whose effective value on a tracked ref
	// differs from the default branch for one environment. The fact carries
	// keys plus per-env divergence booleans only — no values, no hashes —
	// so a divergence answer can never leak a secret. See issue #5393.
	ConfigKeyDivergenceFactKind = "config_key_divergence"

	// RefOverlaySchemaVersionV1 is the first ref_overlay fact schema.
	RefOverlaySchemaVersionV1 = "1.0.0"
)

var refOverlayFactKinds = []string{
	RefOverlayBranchObservationFactKind,
	ConfigKeyDivergenceFactKind,
}

var refOverlaySchemaVersions = map[string]string{
	RefOverlayBranchObservationFactKind: RefOverlaySchemaVersionV1,
	ConfigKeyDivergenceFactKind:         RefOverlaySchemaVersionV1,
}

// RefOverlayFactKinds returns the accepted ref_overlay fact kinds in their
// emission order.
func RefOverlayFactKinds() []string {
	return slices.Clone(refOverlayFactKinds)
}

// RefOverlaySchemaVersion returns the schema version for a ref_overlay fact
// kind.
func RefOverlaySchemaVersion(factKind string) (string, bool) {
	version, ok := refOverlaySchemaVersions[factKind]
	return version, ok
}

// RefOverlayScopeID returns the ref-qualified scope for overlay facts on
// one tracked ref, per the #5393 isolation contract: the canonical git
// repository scope prefix (scope.GitRepositoryScopePrefix, single-sourced
// per #7879) plus the repo_id plus "@<ref>". The @ref qualifier is
// structural: an overlay fact scoped this way can never join a canonical
// row keyed on the bare repo_id.
func RefOverlayScopeID(repoID, ref string) string {
	return scope.GitRepositoryScopePrefix + repoID + "@" + ref
}

// RefOverlayFamilyDeclaration is the typed fact-family contract declaration
// for the ref_overlay family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives the
// registry, shards, and catalog from it.
//
// Both kinds are disclosed DispositionUnconsumed: this leaf ships the
// contract only, and the generator rejects a kind with neither a reducer
// binding nor a disposition. The ref-extraction emitter and the ref-scoped
// read surface that consume these kinds land in sibling Phase 1 leaves of
// eshu-hq/eshu#5393; until they do, read_surface stays "none" (the
// recognized no-claim sentinel) so the consumer-existence gates have nothing
// to resolve.
var RefOverlayFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "ref_overlay",
	FactDir:                "go/internal/facts",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "ref_overlay_isolation",
	ProjectionHook:         "ref_overlay_isolation",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "none",
	TruthProfile:           "deterministic",
	ProviderKeyIndependent: true,
	Kinds: []contract.Kind{
		{
			Kind:              "ref_overlay.branch_observation",
			PayloadSchema:     "sdk/go/factschema/schema/ref_overlay.branch_observation.v1.schema.json",
			Disposition:       contract.DispositionUnconsumed,
			NoReducerReason:   "Phase 1 contract-only kind for eshu-hq/eshu#5393: the ref-extraction emitter and the ref-scoped read surface land in sibling Phase 1 leaves, so no reducer decodes this kind yet and branch observations produce zero canonical rows.",
			NoReducerEvidence: "rg -n RefOverlayBranchObservationFactKind go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g *.go --glob !*_test.go -> 0 matches at declaration time; same for the wire string ref_overlay.branch_observation",
		},
		{
			Kind:              "config_key_divergence",
			PayloadSchema:     "sdk/go/factschema/schema/config_key_divergence.v1.schema.json",
			Disposition:       contract.DispositionUnconsumed,
			NoReducerReason:   "Phase 1 contract-only kind for eshu-hq/eshu#5393: the config-divergence emitter and its dedicated divergence surface land in sibling Phase 1 leaves, so no reducer decodes this kind yet.",
			NoReducerEvidence: "rg -n ConfigKeyDivergenceFactKind go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g *.go --glob !*_test.go -> 0 matches at declaration time; same for the wire string config_key_divergence",
		},
	},
	Producers: []contract.Producer{
		{ID: "git-core", Repository: "eshu-hq/eshu", Path: "go/internal/collector/repo", AuthorityMode: contract.AuthorityCoreOwned, Kinds: []string{"ref_overlay.branch_observation", "config_key_divergence"}},
	},
})
