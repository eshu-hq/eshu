// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package iamcan projects AWS IAM permission facts into the two canonical
// "what can this principal actually do" graph edges: CAN_ASSUME (a principal
// may assume a role, read from trust policies) and CAN_PERFORM (a principal
// holds a catalogued sensitive action against exactly one scanned resource).
//
// The family owns two handlers,
// [IAMCanAssumeMaterializationHandler] and [IAMCanPerformMaterializationHandler],
// registered additively through [AssumeMaterializationDomainDefinition] and
// [PerformMaterializationDomainDefinition]. Both are additive rather than
// default because each needs an explicitly wired edge writer
// ([IAMCanAssumeEdgeWriter], [IAMCanPerformEdgeWriter]) and a fact loader;
// registering either without them would accept intents and drop every one.
//
// Both slices are deliberate under-approximations. An edge is emitted only
// when the grant is unconditioned, un-denied, and the target resolves to
// exactly one scanned CloudResource node of the expected type. Wildcards,
// ambiguity, conditions, permission boundaries, and unscanned targets all
// degrade to a counted skip, never to a guessed edge, so a CAN_PERFORM edge in
// the graph is a claim an operator can act on.
//
// [CatalogByAction] exposes the closed CAN_PERFORM action catalog. The reducer
// root's INVOKES_CLOUD_ACTION intent builder reads it as a defense-in-depth
// check that a code call site can never name an action outside the reviewed
// vocabulary.
//
// # Package boundary
//
// Imports point strictly downward: this package reaches [reducercontract],
// [cloudjoin], [factdecode], [factload], [gpphase], [iampolicy],
// [payloadcore], [schemadecode], internal/facts, internal/graph/edgetype,
// internal/telemetry, internal/truth, and the factschema SDK, and never the
// parent internal/reducer package. The reducer root keeps compatibility
// aliases in iam_can_compat.go so the reducer command and the cypher writers
// compile unchanged; that direction is root importing this family, never the
// reverse. See AGENTS.md in this directory before adding an import.
//
// # Observability
//
// The CAN_ASSUME handler emits eshu_dp_iam_can_assume_edges_total (by
// principal_kind and resolution_mode). The CAN_PERFORM handler emits
// eshu_dp_iam_can_perform_edges_total (by resolution_mode),
// eshu_dp_iam_can_perform_skipped_total (by skip_reason), and
// eshu_dp_iam_can_perform_conditioned_total (by confidence). Facts rejected
// for a malformed payload increment the shared
// eshu_dp_reducer_input_invalid_facts_total counter, and the reducer
// executions that run these handlers stay covered by
// eshu_dp_reducer_executions_total and eshu_dp_reducer_run_duration_seconds.
package iamcan
