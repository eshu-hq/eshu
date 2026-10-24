// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package sbt extracts repository-side build.sbt dependency evidence for the
// supply-chain impact reducer.
//
// Parse reads one build.sbt and emits one content_entity-shaped "variables"
// row per literal libraryDependencies coordinate. A row is "resolved" only
// when its whole statement matches a closed grammar (README.md, "Grammar"):
// a bare libraryDependencies key at the top level of the file (a line whose
// first token is at column 0) or as an argument of a project's
// `.settings(...)`, then `+=` with one element or
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
// brace block, an indented line at depth zero (sbt 2.x reads build.sbt as
// Scala 3, where that is a braceless val, def, if, match, for or while body),
// a parenthesised or dotted call, a repeated or empty `% config`, a
// collection call (`.map`, `++`), a string holding a backslash, and a
// statement that runs on past its end, `<+=`, `<++=`, and any other Scala
// assignment operator. A `:=`, `~=`, `-=`, `--=` or sbt 0.13 `<<=`, or a member
// that sbt's SettingKey API defines to rewrite the key (`set`, `set0`,
// `transform`, `remove1`, `removeN`, and `withRank` before `:=`, `~=`, `-=`,
// `--=` or `<<=`), supersedes the earlier rows it can reach, which become
// partial too. Such a member is applied wherever it appears, since the Setting
// it returns changes the key wherever it is used; outside a grammar context it
// supersedes every row. Both keys are default-deny: an occurrence that is no
// recognised statement and no read (`.value`, `.in(Test).value`,
// `(k in Test).value`) is a rewrite. For libraryDependencies that includes a
// parenthesized key, chained `in` axes, an infix member (`set`, `withRank`),
// a member that returns no Setting (`map`, `filter`), and a bare key; it
// supersedes every row and is counted. A key that a statement handler consumes
// itself is judged by a watch in the scanner that follows the same read
// grammar (axes, `in` chains, `.in(...)` and closers before a read member),
// one token at a time; an inner occurrence whose verdict its statement
// already applied is not counted again. A key named as a plain element of an
// allow-listed key-list setting (buildInfoKeys, excludeLintKeys,
// includeLintKeys) whose list ends the statement is a read. Only a val that heads
// a column-0 line resolves a version reference.
//
// Configuration lists split on ',' and ';' and resolve once, matching names
// exactly as Ivy does (`"Test"` is not `test`): any compile part gives
// compile, runtime without compile gives runtime. Each row mirrors the
// Gradle row contract: the organization:name identity in name, the revision
// in value, an explicit dependency_resolution_state of "resolved", "partial"
// (outside the grammar, an unmapped configuration, or an Ivy dynamic
// revision) or "unresolved" (a non-literal revision), and package_manager
// "sbt".
//
// The payload also carries an sbt_state envelope with the scalaVersion, the
// number of distinct scalaVersion literals, the emitted
// dependency count, the count of valid declarations dropped past
// MaxDependencyRows, the count of declarations that were recognised but could
// not be read as resolved literals, and meta_build. A scalaVersion literal is
// read only in a grammar context (or behind a `ThisBuild /` axis that starts a
// statement in one); a nested one is counted, never recorded. The envelope
// names a version, and `%%` rows get a binary-artifact hint, only when exactly
// one distinct literal was recorded and no other scalaVersion occurrence was
// seen but a read: an unreadable one in a grammar context (a non-literal value, `~=`,
// `<<=`, `.set`, `.transform`, `.withRank(...) :=`, `.:=(...)`) or any
// assignment outside one (a settings val or def, `inThisBuild(...)`, a
// wrapper, a lambda, another axis), or any other occurrence (a parenthesized
// key, chained axes, an infix member, a bare key) poisons the envelope for the
// whole file. meta_build is
// true for a build.sbt directly inside a `project` directory, sbt's
// meta-build, and every row of that file is partial.
//
// sbt build files are Scala. The parser never evaluates Scala or runs sbt,
// reads no sibling file, and uses a single-pass bounded scanner, so work and
// allocation are linear in the input for any line length, nesting depth, tail
// length, or configuration length.
package sbt
