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
	// the emission; the runtime_declaration_projection reducer domain (#8816)
	// projects it to Repository-[:DECLARES_RUNTIME]->RuntimeDeclaration; the
	// query tool (#8817) reads it through POST /api/v0/runtime-declarations/query.
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
// The reducer domain runtime_declaration_projection (#8816) consumes the kind
// and projects Repository-[:DECLARES_RUNTIME]->RuntimeDeclaration. ReadSurface
// names the query route (#8817) that serves it.
var RuntimeDeclarationFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "runtime_declaration",
	FactDir:                "go/internal/facts",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "runtime_declaration_projection",
	ProjectionHook:         "runtime_declaration_projection",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "POST /api/v0/runtime-declarations/query",
	TruthProfile:           "deterministic",
	ProviderKeyIndependent: true,
	Kinds: []contract.Kind{
		{Kind: "runtime_declaration.version", PayloadSchema: "sdk/go/factschema/schema/runtime_declaration.version.v1.schema.json"},
	},
	Producers: []contract.Producer{
		{ID: "git-core", Repository: "eshu-hq/eshu", Path: "go/internal/collector/repo", AuthorityMode: contract.AuthorityCoreOwned, Kinds: []string{"runtime_declaration.version"}},
	},
})
