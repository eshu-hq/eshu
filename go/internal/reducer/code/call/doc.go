// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package call holds the code-call dispatcher: row extraction from parser
// file facts, generic resolution dispatch, explicit per-language resolver
// wiring, Python metaclass edge extraction (delegated to
// code/call/python), and the shared-intent rows and refresh-partition keys
// the code-call projection runner consumes (issue #6061, moved under #6609,
// nested by language under #6609's follow-up language-nesting decision).
//
// The entity-index substrate lives in code/call/shared, and each parser
// language's call resolver lives in its own leaf package under code/call
// (code/call/golang, code/call/java, code/call/javascript, and so on).
// languages.go wires each leaf's exported resolver list into
// codeCallLanguageResolvers under the language key(s) parser output uses;
// there is no init()-time registration left in this family. No leaf imports
// this package, and code/call/shared imports no leaf.
//
// The reducer root imports this package as codecall. [materialization]
// (code/call/materialization, issue #6061) holds the handler
// (materialization.Handler, root spelling CodeCallMaterializationHandler),
// which calls ExtractAllRelationshipRowsWithIndex, BuildFileScopesByRepoID,
// BuildRefreshIntentsWithDeltaFileScopes, and BuildSharedIntentRows here and
// composes the result with the symbol-runtime builders that moved with it
// (materialization.BuildIntentRows). The seven code_call_projection_* runner
// files still stay in root: they read PartitionKeyVersion, PayloadBool, and
// the evidence-source constants through the parent's compat_projection.go
// stanza, and code_call_projection_runner.go imports this package directly
// for AcceptanceScanLimit. External callers keep the reducer.ExtractCodeCallRows
// and reducer.ExtractAllCodeRelationshipRows spellings through that same
// stanza.
//
// shared.EntityIndex is the shared substrate: the resolvers, the materialization
// helpers, and [materialization]'s handles_route, runs_in, invokes_cloud_action,
// and symbol-runtime builders all resolve code entities through it. It is built
// once per materialization pass by shared.BuildEntityIndex (or returned by
// ExtractAllRelationshipRowsWithIndex) and is read-only after construction;
// its language-specific fields stay unexported and are read through accessor
// methods so the invariant survives the package boundary.
//
// Dependency rule: from the reducer tree this package imports code/call/shared
// and the code/call language leaves, plus the shared tier (contract, factload,
// factdecode, schemadecode, sharedintent, payloadcore); outside it, facts,
// codeprovenance, the SDK factschema, and the standard library. It never
// imports the parent reducer package. File names carry no family prefix: the
// directory already says code/call, so code_call_materialization_extract.go
// became extract.go and code_call_language_dart_resolver.go became
// dart/resolver.go under its language leaf.
package call
