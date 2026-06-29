// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package scala extracts Scala source facts for the parent parser engine.
//
// Parse returns classes, objects, traits, functions, variables, imports, calls,
// exact literal Play/http4s route entries, and bounded dead-code root metadata
// without importing the parent parser package. Root metadata is syntax-backed
// only; unresolved Scala semantics such as macros, implicits, givens, compiler
// plugins, dynamic dispatch, generated routes, and broader http4s extractor
// shapes remain exactness blockers for the query layer.
package scala
