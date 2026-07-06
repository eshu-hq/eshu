// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package demospec loads the demo-first-answers manifest
// (specs/demo-first-answers.v1.yaml), the acceptance oracle for issue #4741
// (epic #4592, "first-run experience"). The manifest pins exactly five demo
// questions to bounded, already-shipped read surfaces (a query playbook, an
// MCP tool, a CLI verb, or an HTTP route) and to the golden-corpus-gate
// artifacts (cassette families under testdata/cassettes/ and fixture repos
// under tests/fixtures/ecosystems/) that back each answer.
//
// # Why this package exists
//
// specs/ lives outside the Go module (go/go.mod), so the manifest cannot be
// embedded with go:embed. LoadManifest reads it from disk with an explicit
// repository-relative path instead, mirroring the pattern in
// go/internal/replaycoverage.LoadDepthRequirements.
//
// # What LoadManifest guarantees
//
// A successfully loaded Manifest always has exactly five Questions, each with
// a non-blank ID, question text, correlation kind, and surface ref; a surface
// Kind of playbook, mcp, cli, or http; at least one expected-answer field or
// JSON path; a non-negative minimum_results; and at least one demonstrated
// correlation ID. A surface the gate cannot call directly (a playbook id, or a
// cli verb) additionally carries an execute target — the underlying mcp tool or
// http route the demo-answers golden-gate phase invokes. LoadManifest does
// not check that referenced artifacts (cassette families, fixture repos,
// playbook IDs, or query-shape keys) actually exist — that referential
// integrity is the job of the package's test suite, which cross-checks the
// manifest against query.PlaybookCatalog() and
// testdata/golden/e2e-20repo-snapshot.json so the manifest can never silently
// drift from the corpus and read surfaces it claims to pin down.
package demospec
