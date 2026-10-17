// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "github.com/eshu-hq/eshu/go/internal/facts/contract"

// CodeFamilyDeclaration is the typed fact-family contract declaration for the
// code family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var CodeFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "code",
	FactDir:                "go/internal/facts",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	AdmissionExempt:        true,
	ReducerDomain:          "code_graph_projection",
	ProjectionHook:         "canonical_code_graph",
	AdmissionHook:          "none",
	ReadSurface:            "GET /api/v0/repositories",
	TruthProfile:           "deterministic",
	ProviderKeyIndependent: true,
	Kinds: []contract.Kind{
		{Kind: "file", ReadSurface: "list_repository_files", PayloadSchema: "sdk/go/factschema/schema/file.v1.schema.json"},
		{Kind: "repository", PayloadSchema: "sdk/go/factschema/schema/repository.v1.schema.json"},
	},
	Producers: []contract.Producer{
		{ID: "git-parser-core", Repository: "eshu-hq/eshu", Path: "go/internal/parser", AuthorityMode: "core_owned", Kinds: []string{"file", "repository"}},
	},
})
