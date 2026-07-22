// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package submodule parses a repository's ".gitmodules" file into durable
// "submodule.pin" facts (issue #5420 Phase 2a, epic #5415).
//
// The package implements the Phase 2a producer half of the submodule-graph
// domain. Phase 1 already shipped the fact contract
// (facts.SubmodulePinFactKind, facts.SubmoduleSchemaVersionV1, and the typed
// sdk/go/factschema/submodule/v1.Pin payload); this package turns a parent
// repository's ".gitmodules" file into that contract's fact envelopes. This
// package does not itself resolve the pinned gitlink commit SHA: Emit
// accepts an optional FixtureContext.PinnedSHAResolver callback and fills
// Pin.PinnedSHA per entry when the caller sets one, leaving it nil
// otherwise, but the resolver's git-tree read
// (gitSubmoduleGitlinkSHA, a local "git ls-tree HEAD" read) lives in the Git
// collector (go/internal/collector/git_submodule_pinned_sha.go, issue #5420
// Phase 2b), not here. The reducer, projector, and read surface that
// consume these facts are also out of scope (later phases of the same
// epic).
//
// Four pieces compose the producer:
//
//   - Parse reads one ".gitmodules" file body into an ordered []Entry,
//     following git-config's documented syntax for the subset this
//     collector needs (see parser.go's doc comment for the exact grammar
//     and what is deliberately out of scope).
//   - IsGitmodulesPath recognizes the single repo-relative location git ever
//     reads submodule declarations from: unlike CODEOWNERS' three possible
//     locations, ".gitmodules" is always exactly one file at the repository
//     root.
//   - ResolveRepoID resolves one submodule's raw URL to Eshu's canonical
//     repo_id when — and only when — that URL is an absolute, host-qualified
//     git remote. It returns the empty string ("no durable link") for git's
//     own relative submodule URL forms and anything else it cannot
//     canonicalize, following the same never-guess discipline
//     go/internal/collector/jira/linked_repository.go applies to PR/MR
//     links.
//   - Emit turns one ".gitmodules" file's parsed entries into
//     "submodule.pin" fact envelopes, one per entry, keyed by
//     (parent_repo_id, submodule_path), calling
//     FixtureContext.PinnedSHAResolver per entry when the caller set one
//     (issue #5420 Phase 2b).
//
// This package does not select which files reach it, decide repository
// scope/generation identifiers, or call the reducer or query packages.
// Candidate-file discovery, gitlink SHA resolution, and the fact-stream
// wiring live in the Git collector (go/internal/collector), which calls
// IsGitmodulesPath during content discovery, accumulates the candidate body
// during its content stream, and calls Emit once per repository generation
// with a PinnedSHAResolver bound to gitSubmoduleGitlinkSHA.
package submodule
