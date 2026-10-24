// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sbt extracts repository-side build.sbt dependency evidence for the
// supply-chain impact reducer.
//
// Parse reads one build.sbt and emits one content_entity-shaped "variables"
// row per literal libraryDependencies coordinate. A row is "resolved" only
// when its whole statement matches a closed grammar (README.md, "Grammar"):
// a bare libraryDependencies key at the top level of the file or as an
// argument of a project's `.settings(...)`, then `+=` with one element or
// `++=`/`:=` with a Seq, List or Vector of elements. An element is
// `"org" (%|%%|%%%) "name" % revision`, at most one `% config` (a string or
// Compile, Test, Runtime, Provided, Optional, IntegrationTest), and infix calls
// from a small allowlist of ModuleID methods that cannot change coordinates or
// configurations (exclude, excludeAll, classifier, intransitive, withSources,
// withJavadoc, cross, force, changing).
//
// Every other statement takes the fail-closed fallback: each literal
// coordinate found in it is emitted "partial" with a placeholder scope and
// counted in sbt_state.unresolved_declaration_count, never resolved. That
// covers any axis or wrapper (`Test /`, `ThisBuild /`, `inConfig(...)`), a
// brace block, a parenthesised or dotted call, a repeated or empty `% config`,
// a collection call (`.map`, `++`), and a statement that runs on past its end.
// A `:=`, `~=`, `-=` or `--=` supersedes the earlier rows it can reach, which
// become partial too.
//
// Configuration lists split on ',' and ';' and resolve once: any compile part
// gives compile, runtime without compile gives runtime. Each row mirrors the
// Gradle row contract: the organization:name identity in name, the revision
// in value, an explicit dependency_resolution_state of "resolved", "partial"
// (outside the grammar, an unmapped configuration, or an Ivy dynamic
// revision) or "unresolved" (a non-literal revision), and package_manager
// "sbt".
//
// The payload also carries an sbt_state envelope with the literal
// scalaVersion, the number of distinct scalaVersion literals, the emitted
// dependency count, the count of valid declarations dropped past
// MaxDependencyRows, and the count of declarations that were recognised but
// could not be read as resolved literals.
//
// sbt build files are Scala. The parser never evaluates Scala or runs sbt,
// reads no sibling file, and uses a single-pass bounded scanner, so work and
// allocation are linear in the input for any line length, nesting depth, tail
// length, or configuration length.
package sbt
