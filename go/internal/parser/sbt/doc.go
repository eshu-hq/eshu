// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sbt extracts repository-side build.sbt dependency evidence for the
// supply-chain impact reducer.
//
// Parse reads one build.sbt and emits one content_entity-shaped "variables"
// row per literal libraryDependencies coordinate. It accepts the `+=`, `++=`
// and `:=` operators, the `%`, `%%` and `%%%` coordinate forms, an optional
// configuration operand (`% Test`, `% "provided"`), and a `Config /` scope
// axis before the key. Each row mirrors the Gradle row contract: the
// organization:name identity in name, the revision in value, an explicit
// dependency_resolution_state of "resolved", "partial" (no revision or an Ivy
// dynamic revision) or "unresolved" (a non-literal revision), and
// package_manager "sbt".
//
// The payload also carries an sbt_state envelope with the literal
// scalaVersion, the number of distinct scalaVersion literals, the emitted
// dependency count, the count of valid declarations dropped past
// MaxDependencyRows, and the count of declarations that were recognised but
// could not be read as literals.
//
// sbt build files are Scala. The parser never evaluates Scala or runs sbt,
// reads no sibling file, and uses a single-pass bounded scanner, so work is
// linear in the input for any line length or nesting depth.
package sbt
