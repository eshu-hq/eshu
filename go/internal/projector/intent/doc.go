// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package intent defines the dependency-neutral contract between projector
// assembly and reducer-intent family builders.
//
// ReducerIntent carries one shared-domain work request. FactLookup indexes an
// immutable fact generation without importing the root projector package.
// SourceSystem applies the common source-ref-first label rule. Lookups preserve
// original fact order when selecting the first match, including selections
// across several fact kinds.
package intent
