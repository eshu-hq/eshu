// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

const (
	// SubmodulePinFactKind identifies one declared or observed git submodule
	// pin: a parent repository's .gitmodules/gitlink entry for one submodule
	// path, carrying the raw submodule URL (when declared) and/or the pinned
	// commit SHA (when a gitlink exists). Phase 1 of issue #5420 defines this
	// contract only; the collector emission and reducer/graph consumption
	// land in later phases of the same epic.
	SubmodulePinFactKind = "submodule.pin"

	// SubmoduleSchemaVersionV1 is the first submodule fact schema.
	SubmoduleSchemaVersionV1 = "1.0.0"
)

var submoduleFactKinds = []string{
	SubmodulePinFactKind,
}

var submoduleSchemaVersions = map[string]string{
	SubmodulePinFactKind: SubmoduleSchemaVersionV1,
}

// SubmoduleFactKinds returns the accepted submodule fact kinds in their
// emission order.
func SubmoduleFactKinds() []string {
	return slices.Clone(submoduleFactKinds)
}

// SubmoduleSchemaVersion returns the schema version for a submodule fact
// kind.
func SubmoduleSchemaVersion(factKind string) (string, bool) {
	version, ok := submoduleSchemaVersions[factKind]
	return version, ok
}

// SubmoduleFamilyDeclaration is the typed fact-family contract declaration for the
// submodule family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var SubmoduleFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "submodule",
	FactDir:                "go/internal/facts",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "submodule_pin",
	ProjectionHook:         "submodule_pin",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "none",
	TruthProfile:           "deterministic",
	ProviderKeyIndependent: true,
	Kinds: []contract.Kind{
		{Kind: "submodule.pin", PayloadSchema: "sdk/go/factschema/schema/submodule.pin.v1.schema.json"},
	},
	Producers: []contract.Producer{
		{ID: "git-core", Repository: "eshu-hq/eshu", Path: "go/internal/collector/repo", AuthorityMode: "core_owned", Kinds: []string{"submodule.pin"}},
	},
})
