// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package refreshworkflow provides the locally-provable tests for the
// R-6 credentialed cassette refresh workflow (epic #4102, issue #4108).
//
// Scope: this package is narrowly scoped to two runtime properties of the
// refresh workflow. It is distinct from go/internal/replay/schema, which
// validates cassette JSON structure and schema-version compliance. This
// package does not validate schema; it proves that the diff produced by a
// live re-record is useful (legible) and safe (redacted).
//
// The refresh workflow runs on a hosted GitHub Actions runner with real provider
// credentials, invokes -mode=record on collector binaries to regenerate cassettes
// from live APIs, and opens or updates a pull request whose diff is the canonical
// diff against the committed fixtures. Two properties must hold for that diff to
// be useful and safe:
//
//  1. Canonical-diff legibility: because cassettes are canonicalized (sorted
//     keys, sorted arrays, volatile fields collapsed) a re-record with an
//     identical fact shape produces an empty diff. When a fact field changes, the
//     diff is small and line-level — not whole-file churn — so a reviewer can
//     reason about what actually changed in the provider API or schema. This
//     property is proved by TestCanonicalDiffIsLegible.
//
//  2. Redaction: secrets never appear in the recorded artifacts. The recorder
//     calls replay.Canonicalize with configured RedactKeys so credential-bearing
//     fields are replaced with replay.RedactedSentinel ("<redacted>") before the
//     cassette is written. Redaction is opt-in per key: a key omitted from
//     RedactKeys is preserved verbatim (see TestUnregisteredSecretKeyLeaks). The
//     credentialed CI job adds no new redaction logic — it uses the same
//     Canonicalize path. This property is proved by TestRedactionNeverLeaksSecrets
//     and TestUnregisteredSecretKeyLeaks.
//
// Neither test requires credentials or network access. The credentialed
// recording itself can only run in CI with real provider secrets; local tests
// assert the properties the CI job depends on.
package refreshworkflow
