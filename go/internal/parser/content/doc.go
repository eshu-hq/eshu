// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package content infers file content classification for the parser engine:
// artifact type, template dialect, and IaC relevance, plus the skip gate that
// lets ParsePath avoid the inference cost for files that cannot produce a
// non-zero result.
//
// Infer classifies one file from its path and bytes alone. It is a pure
// function: the same path and content always yield the same Metadata, which
// is what makes the ShouldSkip gate sound. ShouldSkip reports whether the
// caller may use the Metadata{} zero value without changing the persisted
// result; it is intentionally conservative and returns false whenever any
// extension, basename, path-segment, or content-marker check could match.
// The exhaustive generative equivalence test in gate_test.go pins that
// conservatism: every generated extension, directory, and content signal
// asserts byte-identical results against the unconditional Infer call.
//
// The package depends only on the standard library. It must not import the
// parent internal/parser package: the engine calls in, never the reverse.
// Suffix splitting lives here as SplitSuffixes, shared with the raw-text
// language fallback in the parent package so both route on the same
// dot-suffix semantics.
//
// No-Regression Evidence: moving inference behind ShouldSkip changes no
// persisted payload. The gate is a pure skip-or-compute decision over the
// same Infer both branches call, and the equivalence test fails on any input
// where the gate would skip a file Infer classifies non-zero (issue #4768).
//
// No-Observability-Change: this package emits no metric, span, status field,
// or runtime knob of its own. Inference cost is visible through the existing
// file parse duration metric on the calling path.
package content
