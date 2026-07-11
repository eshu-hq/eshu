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
package reportbundle
