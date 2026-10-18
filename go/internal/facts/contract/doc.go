// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package contract is the dependency-neutral metadata model for typed,
// colocated fact-family declarations (issue #6629). A [Family]
// declaration lives beside the family's fact definitions and names the
// family, its fact-definition directory, its canonical core owner
// repository, its kinds and per-kind metadata, its current producers
// with their emitted-kind subsets, and its read surfaces and schema
// references. A [Binding] declaration lives beside a reducer
// implementation and names the exact kind subset that implementation
// consumes. Family identity, fact-definition directory, reducer
// ownership, producer identity, and read surfaces are separate
// relationships in this model on purpose: several families may share a
// reducer package or domain, and one producer may emit several
// families.
//
// Declarations are build-time source of truth. The fact-kind-registry
// generator (go/cmd/fact-kind-registry) discovers them with a static
// go/parser scan over a restricted literal grammar:
//
//	var someFamily = contract.Declare(contract.Family{
//	    ID: "...", FactDir: "...", ...,
//	    Kinds: []contract.Kind{{Kind: "..."}},
//	    Producers: []contract.Producer{{ID: "...", Kinds: []string{"..."}}},
//	})
//
// Every field value must be a string or boolean literal, a
// <package-name>.<Const> reference to a string constant declared in
// this package (for example contract.DispositionReadOnly, so
// declarations get compile-checked spellings), a slice literal of
// literals, or a nested struct literal in this package. Variables,
// function calls, and computed expressions are rejected with the
// declaration's file and line. The scan parses source only: it never
// compiles, loads, or executes the scanned packages, so package
// initialization code cannot run during discovery by construction.
//
// Generated outputs (the compact specs/fact-kind-registry.v1.yaml, the
// specs/fact-kind-registry-contracts.d shards, the generated Go registry
// and Markdown tables) are derived from these declarations. Nothing in
// this package runs at admission, reducer, projection, or query time;
// [Declare] and [Bind] are identity markers, and the model imports
// nothing from the rest of the module so fact families and reducer
// packages can both declare against it without an import cycle.
//
// Invariants the generator enforces (declared here so contributors can
// predict failures): each kind has exactly one canonical owning family;
// each kind is either covered by one or more verified bindings or
// carries a non-blank disposition with a reason and evidence; a family
// declaration file sits in its FactDir; a binding file sits in its
// ReducerDir and names a real top-level symbol there; producer subsets
// name only kinds the family owns. The generator's diagnostics name the
// declaration file and line, any conflicting source location, and the
// corrective action.
package contract
