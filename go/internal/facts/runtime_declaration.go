// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

const (
	// RuntimeDeclarationVersionFactKind identifies one runtime version a
	// repository declares in one file: a version-pin file such as .nvmrc or
	// .tool-versions, the engines, volta and packageManager sections of
	// package.json, or the go and toolchain directives of go.mod, with
	// Dockerfile and CI setup sources following. A line that lists several
	// versions is one fact per token. Issue #8815 defines the contract and
	// the emission; the graph projection (#8816) and the query tool (#8817)
	// land separately, so nothing reads this kind yet.
	RuntimeDeclarationVersionFactKind = "runtime_declaration.version"

	// RuntimeDeclarationSchemaVersionV1 is the first runtime_declaration fact
	// schema.
	RuntimeDeclarationSchemaVersionV1 = "1.0.0"
)

var runtimeDeclarationFactKinds = []string{
	RuntimeDeclarationVersionFactKind,
}

var runtimeDeclarationSchemaVersions = map[string]string{
	RuntimeDeclarationVersionFactKind: RuntimeDeclarationSchemaVersionV1,
}

// RuntimeDeclarationFactKinds returns the accepted runtime_declaration fact
// kinds in their emission order.
func RuntimeDeclarationFactKinds() []string {
	return slices.Clone(runtimeDeclarationFactKinds)
}

// RuntimeDeclarationSchemaVersion returns the schema version for a
// runtime_declaration fact kind.
func RuntimeDeclarationSchemaVersion(factKind string) (string, bool) {
	version, ok := runtimeDeclarationSchemaVersions[factKind]
	return version, ok
}

// RuntimeDeclarationFamilyDeclaration is the typed fact-family contract
// declaration for the runtime_declaration family (issue #6629). It is
// build-time metadata: the fact-kind-registry generator discovers it
// statically and derives the registry, shards, and catalog from it.
//
// The kind is disclosed DispositionUnconsumed: this change ships the contract
// and the pin-file emission only, and the generator rejects a kind with
// neither a reducer binding nor a disposition. Issue #8816 adds the reducer
// binding that projects Repository-[:DECLARES_RUNTIME]->RuntimeDeclaration and
// removes the disposition. Until then read_surface stays "none", the
// recognized no-claim sentinel.
var RuntimeDeclarationFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "runtime_declaration",
	FactDir:                "go/internal/facts",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "runtime_declaration_projection",
	ProjectionHook:         "runtime_declaration_projection",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "none",
	TruthProfile:           "deterministic",
	ProviderKeyIndependent: true,
	Kinds: []contract.Kind{
		{
			Kind:              "runtime_declaration.version",
			PayloadSchema:     "sdk/go/factschema/schema/runtime_declaration.version.v1.schema.json",
			Disposition:       contract.DispositionUnconsumed,
			NoReducerReason:   "Contract and pin-file emission for eshu-hq/eshu#8815: the reducer projection (#8816) and the query tool (#8817) land in separate changes, so no reducer decodes this kind yet and declarations produce zero canonical rows.",
			NoReducerEvidence: "rg -n RuntimeDeclarationVersionFactKind go/internal/reducer go/internal/projector go/internal/query go/internal/storage/postgres go/internal/relationships -g *.go --glob !*_test.go -> 0 matches at declaration time; same for the wire string runtime_declaration.version",
		},
	},
	Producers: []contract.Producer{
		{ID: "git-core", Repository: "eshu-hq/eshu", Path: "go/internal/collector/repo", AuthorityMode: contract.AuthorityCoreOwned, Kinds: []string{"runtime_declaration.version"}},
	},
})
