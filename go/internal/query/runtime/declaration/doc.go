// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package declaration implements the declared-runtime-version query family:
// the bounded, graph-backed read of the RuntimeDeclaration nodes the
// runtime_declaration_projection reducer domain writes as
// Repository-[:DECLARES_RUNTIME]->RuntimeDeclaration (issue #8816).
//
// The handler serves POST /api/v0/runtime-declarations/query with
// deterministic keyset pagination over the five-part graph key
// (repo_id, relative_path, line_number, runtime, order_index): a page is
// stable across calls and a cursor resumes strictly after the last row. A
// version comparison (major_below, or version_below with one to three
// components) requires the runtime filter, returns only the declarations it
// can place (exact and partial specs), and reports the rest -- ranges,
// aliases, unresolved specs, and partial specs too imprecise for the bound --
// in excluded_from_comparison and partial_reasons, never by guessing.
//
// There is no effective-version view: a repository with a root .nvmrc and a
// services/web/.nvmrc returns both rows, each with its scope_dir ("." at the
// repository root).
//
// The same package serves the runtime_declarations section of
// get_repo_context through LoadSection: the declaration count and the
// distinct runtimes with their lowest resolved major. The caller has already
// resolved and authorized the repository; a section failure is returned for
// the caller to degrade on, never rendered as an authoritative "declares
// nothing".
//
// Three contracts callers must hold. Every read is bounded: the limit clamp
// (default 50, max 200) plus the limit+1 truncation probe, the 10s graph
// read budget, and the at-most-100 repo_ids filter. A scoped caller with an
// empty grant gets the bounded empty page without touching the graph, so an
// ungranted repository is indistinguishable from a granted-but-empty one.
// Capability registration stays in root package query
// (contract/capability_matrix.go), which owns the router and always links
// into the production binary; this package only declares the Support
// constructor the family TestMain registers.
//
// A failed graph read never echoes the backend error: a fence or
// graph-availability verdict answers 503/504, a client cancel 499, and
// anything else a fixed per-step 500 with the error recorded on the handler
// span (#7674).
//
// See README.md for the full boundary and AGENTS.md for the per-symbol
// export list.
package declaration
