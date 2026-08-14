// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package reportbundle composes, redacts, digests, and validates deterministic
// wrong_answer_report.v1 artifacts (report bundles).
//
// A report bundle is not a graph export, a fixture, or an Ifá Odù — it is a
// share-safe snapshot of one query/response pair (surface, target, params,
// the verbatim query.TruthEnvelope, redacted response data, its
// replay-equality digest, and evidence references) that a user attaches to a
// wrong-answer issue report. Slice 2 (a later change) converts a
// maintainer-confirmed bundle into an Ifá Odù conformance case; this package
// only owns the bundle itself.
//
// # Redaction domains
//
// Redaction is scoped by where a value came from, not by how it reached the
// bundle. Reporter-typed input covers Query.Target, all of Query.Params,
// Response.Error.Details (which echoes the caller's own selector back), and
// ReporterNote. The first three are structured, so they get the
// sensitive-key-name walk plus a re-parse of any query-string-shaped value at
// any depth, as typed and once percent-decoded (redactQueryInput).
//
// Free text gets a line-by-line scan for a sensitive-named key beside an "=" or
// a ":" (redactFreeText), each read through one layer of percent-encoding so
// "%3D" counts as an "=". ReporterNote is in that domain because the guide asks
// reporters for a repro, so it commonly holds a pasted curl. So are
// Response.Error.Message and Response.Error.CorrelationID: a Message is composed
// server-side out of the caller's own selector, and a CorrelationID is the
// caller's own X-Correlation-ID header when one was sent. "Composed by the
// server" is not the same as "free of reporter bytes", and treating it as such
// is what shipped a credential inside a bundle whose details.selector had just
// been redacted for holding the identical string.
//
// Server-produced evidence — Response.Data and Response.Truth — gets the
// key-name walk only, because judging its content would strip the answer the
// bundle exists to carry; see the package README for what that exemption costs.
//
// Every one of these rules asks the same question: is there a KEY NAME here
// that collector.IsSensitiveKeyName flags. What differs between them is where
// key names are looked for. None of them judges what a value looks like, and no
// secret-pattern or entropy heuristic belongs in any of them.
//
// # Error rule
//
// No user-supplied string is interpolated into an error this package returns.
// Errors name the field ("query.target", "query.params.next") and never repeat
// its value: they land in terminals, CI logs, and pasted bug reports, which are
// the same places the bundle beside them is redacted for. A parameter name is
// reporter-typed too, and a name can itself be a "key=value" pair, so the ones
// that are get replaced with a fixed marker before they reach a message (see
// safePathSegment).
package reportbundle
