// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package shared holds the code-call substrate every per-language resolver
// package needs: EntityIndex (the built lookup structure), the generic
// candidate-name and path-normalization helpers, the import/reexport
// resolution helpers, and the per-language index contributors that
// [BuildEntityIndex] calls while scanning "file" facts (issue #6061, split
// from the single code/call package per the language-nesting proposal).
//
// EntityIndex is read-only after [BuildEntityIndex] returns it. Its
// language-specific lookup fields stay unexported; callers outside this
// package read them through the accessor methods on EntityIndex
// (EntityFileByID, UniqueNameByRepoDir, GoMethodReturnTypes,
// GoExportByImportPath, JavaScriptAliasesByFile, PythonClassBasesByRepo,
// RustTraitMethodsByRepo, SpansByFile, TypeScriptInterfaceMethodsByRepo) so
// the read-only invariant survives the package boundary.
//
// Span lookups are repository- and file-scoped (#7640): function and type
// spans and the JavaScript alias cache are stored per repository under only
// the file's own [FileKeys] (normalized full path and normalized relative
// path), never under a bare file name. [ResolveContainingEntityID] therefore
// returns "" for a call with no enclosing span in its own file instead of
// borrowing a span from a same-named file elsewhere. Do not add mutation
// methods; the symbol-runtime builders in the reducer root share the same
// instance.
//
// Dependency rule: this package imports only the shared reducer tier
// (contract, factload, factdecode, schemadecode, sharedintent, payloadcore)
// and, outside the reducer, facts, codeprovenance, the SDK factschema, and
// the standard library. It never imports code/call or any code/call language
// leaf; every language leaf imports this package instead (java and kotlin
// additionally import code/call/jvm).
package shared
