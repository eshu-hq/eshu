// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codeowners parses GitHub CODEOWNERS files into durable
// "codeowners.ownership" facts (issue #5419 Phase 2).
//
// The package implements the producer half of the codeowners ownership
// domain. Phase 1 already shipped the fact contract
// (facts.CodeownersOwnershipFactKind, facts.CodeownersSchemaVersionV1, and the
// typed sdk/go/factschema/codeowners/v1.Ownership payload); this package turns
// a repo-hosted CODEOWNERS file into that contract's fact envelopes. The
// reducer, projector, and read surface that consume these facts are out of
// scope here (Phases 3-5).
//
// Three pieces compose the producer:
//
//   - Parse reads one CODEOWNERS file body into an ordered []Rule, following
//     GitHub's documented CODEOWNERS syntax (see parser.go's doc comment for
//     the exact grammar this package implements and what it deliberately
//     leaves out of scope, namely the sections feature).
//   - IsCandidatePath and ResolveWinner implement GitHub's CODEOWNERS
//     location precedence: exactly one of ".github/CODEOWNERS", root
//     "CODEOWNERS", or "docs/CODEOWNERS" is honored per repository, in that
//     order, when more than one is present.
//   - Emit turns the resolved winning file's parsed rules into
//     "codeowners.ownership" fact envelopes, one per rule, keyed by
//     (repo_id, source_path, pattern, order_index).
//
// This package does not select which files reach it, decide repository
// scope/generation identifiers, or call the reducer or query packages.
// Candidate-file discovery and the fact-stream wiring live in the Git
// collector (go/internal/collector), which calls IsCandidatePath during
// content discovery, accumulates candidate bodies during its content stream,
// and calls ResolveWinner and Emit once per repository generation.
package codeowners
