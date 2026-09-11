// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package shell reduces parser command-call evidence into durable
// shared-projection intents for Function-[:EXECUTES_SHELL]->ShellCommand
// (issue #6061). [Handler.Handle] extracts canonical rows
// through [ExtractExecRows], reusing the SQL-relationship family's delta
// scope and repo-ID merge ([sqlrelationship.BuildDeltaScope],
// [sqlrelationship.MergeRepositoryIDs]) rather than duplicating them: both
// families derive the same per-repository delta_generation/
// delta_relative_paths shape from the same "repository" facts.
// [BuildSharedIntentRows] and [BuildRefreshIntents] then build the durable
// per-edge and per-repo-refresh shared-projection intent rows, matching the
// [sqlrelationship] and [inheritance] sibling families' emitter shape so the
// reducer root's cross-domain sibling proofs (delta-gate and
// retract-reachability) can drive all three through one table.
//
// The reducer root imports this package as shell. It keeps the exported
// ShellExecIntentWriter/ShellExecMaterializationHandler spellings and the
// ExtractShellExecRows/loadShellExecMaterializationFacts/
// buildShellExecRefreshIntents/buildShellExecSharedIntentRows forwarders
// through the shell-exec stanza of compat_projection.go.
//
// Dependency rule: from the reducer tree this package imports only the
// shared tier (contract, factload, schemadecode, sharedintent, payloadcore)
// and the sibling leaves sqlrelationship and code/call (for its delta-scope
// helper and PayloadInt); outside it, facts and the standard library. It
// never imports the reducer root.
package shell
