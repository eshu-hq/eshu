// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package producerstore finds the repository scopes that can define a code-call
// symbol key, from the key text and the manifests stored in content_files, so
// the definition loader scans only those scopes' file facts instead of the
// whole corpus (#7601, #7623).
//
// Two kinds of key name their producer. A package:<id>#<export> key is produced
// by the repositories whose package.json names the package. A
// scip-go gomod <import path> <symbol> key is produced by the repositories whose
// go.mod module path is the import path or one of its '/'-prefixes: the Go
// parser builds a definition's import path from its nearest go.mod module path
// plus the package directory, so the defining scope always stores such a go.mod.
// Split sorts keys by kind; every other key has no manifest to name its
// producer and keeps the corpus-wide scan in the caller.
//
// GoModuleName reads a go.mod the way the parser does (after the parser's own
// line-ending normalization, the first line of exactly two fields whose first is
// "module"), so the anchor and the import paths on definitions share one rule.
//
// Store runs the two manifest reads over an injected db.Queryer. This package
// must not import the parent internal/storage/postgres package from non-test
// code (#6693).
//
// The package.json read UNION ALLs the scopes whose stored manifest may be
// ahead of the active generation (#7609); those rows carry a NULL manifest
// and always join the producer set, because a dropped candidate would let a
// same-named producer resolve alone and bypass the ambiguity rule.
package producerstore
