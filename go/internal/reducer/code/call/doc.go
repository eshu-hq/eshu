// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package call holds the code-call family: code-call row extraction from
// parser file facts, the per-language call resolvers, the code-entity index
// they share, Python metaclass edge extraction, and the shared-intent rows
// and refresh-partition keys the code-call projection runner consumes (issue
// #6061, moved under #6609).
//
// The reducer root imports this package as codecall. The root keeps the
// CodeCallMaterializationHandler (code_call_materialization.go), which calls
// ExtractAllRelationshipRowsWithIndex, BuildFileScopesByRepoID,
// BuildRefreshIntentsWithDeltaFileScopes, and BuildSharedIntentRows here and
// composes the result with the symbol-runtime families that stay in root. The
// seven code_call_projection_* runner files also stay in root and read
// PartitionKeyVersion, PayloadBool, and AcceptanceScanLimit through the
// parent's compat_projection.go stanza. External callers keep the
// reducer.ExtractCodeCallRows and reducer.ExtractAllCodeRelationshipRows
// spellings through that same stanza.
//
// EntityIndex is the shared substrate: the resolvers, the materialization
// helpers, and the root's handles_route, runs_in, invokes_cloud_action, and
// symbol-runtime builders all resolve code entities through it. It is built
// once per materialization pass by BuildEntityIndex (or returned by
// ExtractAllRelationshipRowsWithIndex) and is read-only after construction.
//
// Dependency rule: this package imports the shared tier (contract, factload,
// factdecode, schemadecode, sharedintent, payloadcore) plus facts, the SDK
// factschema, and the standard library. It never imports the parent reducer
// package. File names carry no family prefix: the directory already says
// code/call, so code_call_materialization_extract.go became extract.go and
// code_call_language_dart_resolver.go became dart_resolver.go.
package call //nolint:dirgate // code-call family for #6061/#6609: 47 non-test files vs the 40-file cap; the owner chose a single code/call leaf (a separate substrate package was rejected), and EntityIndex is shared by the resolvers, the materialization helpers, and python_metaclass.go, so splitting the directory would cut that substrate across a package boundary.
