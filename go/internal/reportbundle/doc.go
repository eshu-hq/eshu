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
// bundle. Reporter-typed query input — Query.Target, all of Query.Params, and
// Response.Error.Details, which echoes the caller's own selector back — gets
// the sensitive-key-name walk plus a structural re-parse of any
// query-string-shaped value at any depth (redactQueryInput). Server-produced
// evidence — Response.Data and Response.Truth — gets the key-name walk only,
// because judging its content would strip the answer the bundle exists to
// carry; see the package README for what that exemption costs.
//
// # Error rule
//
// No user-supplied string is interpolated into an error this package returns.
// Errors name the field ("query.target", "query.params.next") and never repeat
// its value: they land in terminals, CI logs, and pasted bug reports, which are
// the same places the bundle beside them is redacted for.
package reportbundle
