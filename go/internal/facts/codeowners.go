// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import (
	"slices"

	"github.com/eshu-hq/eshu/go/internal/facts/contract"
)

// CodeownersOwnershipFactKind identifies one CODEOWNERS pattern-to-owners
// mapping: a single line of a repository's CODEOWNERS file, carrying the
// glob pattern, its verbatim owner tokens, and the line's position in the
// file (last-match-wins resolution order). See issue #5419.
const CodeownersOwnershipFactKind = "codeowners.ownership"

// CodeownersSchemaVersionV1 is the first codeowners fact schema.
const CodeownersSchemaVersionV1 = "1.0.0"

var codeownersFactKinds = []string{
	CodeownersOwnershipFactKind,
}

var codeownersSchemaVersions = map[string]string{
	CodeownersOwnershipFactKind: CodeownersSchemaVersionV1,
}

// CodeownersFactKinds returns the accepted codeowners fact kinds in their
// emission order.
func CodeownersFactKinds() []string {
	return slices.Clone(codeownersFactKinds)
}

// CodeownersSchemaVersion returns the schema version for a codeowners fact
// kind.
func CodeownersSchemaVersion(factKind string) (string, bool) {
	version, ok := codeownersSchemaVersions[factKind]
	return version, ok
}

// CodeownersFamilyDeclaration is the typed fact-family contract declaration for the
// codeowners family (issue #6629). It is build-time metadata: the
// fact-kind-registry generator discovers it statically and derives
// the registry, shards, and catalog from it.
var CodeownersFamilyDeclaration = contract.Declare(contract.Family{
	ID:                     "codeowners",
	FactDir:                "go/internal/facts",
	OwnerRepository:        "eshu-hq/eshu",
	LifecycleOwner:         "go/internal/facts",
	SchemaVersion:          "1.0.0",
	ReducerDomain:          "codeowners_ownership",
	ProjectionHook:         "codeowners_ownership",
	AdmissionHook:          "facts.ValidateSchemaVersion",
	ReadSurface:            "GET /api/v0/codeowners/ownership",
	TruthProfile:           "deterministic",
	ProviderKeyIndependent: true,
	Kinds: []contract.Kind{
		{Kind: "codeowners.ownership", PayloadSchema: "sdk/go/factschema/schema/codeowners.ownership.v1.schema.json"},
	},
	Producers: []contract.Producer{
		{ID: "git-core", Repository: "eshu-hq/eshu", Path: "go/internal/collector/repo", AuthorityMode: "core_owned", Kinds: []string{"codeowners.ownership"}},
	},
})
