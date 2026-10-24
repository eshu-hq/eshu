// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sbt extracts repository-side build.sbt dependency evidence for the
// supply-chain impact reducer.
//
// Parse reads one build.sbt and emits one content_entity-shaped "variables"
// row per literal libraryDependencies coordinate. It accepts the `+=`, `++=`
// and `:=` operators, the `%`, `%%` and `%%%` coordinate forms, an optional
// configuration operand (`% Test`, `% "provided"`, also as a leading infix on
// the next line), a trailing configuration after a parenthesised coordinate,
// and a `.map(_ % Test)` over a collection. Configuration lists split on ','
// and ';' and resolve once per configuration: any compile part gives compile,
// runtime without compile gives runtime, and the last of several `% config`
// operands wins.
//
// The default is undetermined. Only a small allowlist of ModuleID methods that
// cannot change the configuration (exclude, classifier, intransitive,
// withSources, cross and the like) keeps a scope readable after a coordinate or
// inside a `.map` body. Any other call, operator or body, a key under a
// configuration axis or an `inConfig(...)` wrapper, an empty configuration, and
// a configuration string over MaxConfigBytes yield a "partial" row that is
// counted, never a resolved compile row. Each row mirrors the Gradle row
// contract: the
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
// work and allocation are linear in the input for any line length, nesting
// depth, tail length, or configuration length.
package sbt
