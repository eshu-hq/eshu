// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package relguard is shared test-support code that mechanizes the AWS scanner
// graph-join contract: every relationship a scanner emits must carry a
// non-empty target_type that names a resource family Eshu can actually resolve.
// It exists so the recurring scanner defect class from issue #804 (empty
// target_type, unknown target_type, ARN-typed target keyed by a bare name) is
// caught mechanically instead of by per-PR human review.
//
// The package has two layers that share one source of truth for the set of
// valid target types (KnownTargetTypes):
//
//  1. A static layer that AST-walks the scanner source tree and collects every
//     statically determinable target_type value: inline string literals, the
//     package and file-local string constants those literals bind to, and the
//     compiler-checked awscloud.ResourceType* selectors. DeclaredResourceTypeValues,
//     EmittedTargetTypeLiterals, Validate, and the one-call ValidateEmitted
//     implement it. The repo-level guard test (TestLiveScannerTreeHasNoGraphJoinDefects)
//     feeds the live tree through it, so a new scanner cannot ship an empty or
//     unknown literal target_type.
//  2. A runtime layer (Check, AssertObservations) that scanner unit tests feed
//     their emitted RelationshipObservation values through. It catches the
//     data-dependent target_type values the static layer cannot see because a
//     helper call or a struct-field read produced them, and additionally checks
//     ARN shape and ARN-vs-name join-mode consistency.
//
// The valid target-type set is the union of every declared awscloud
// ResourceType constant value and the explicit, documented KnownTargetTypeAllowlist
// of forward references and synthetic join anchors. The package intentionally
// has no dependency on the awsruntime registry; the expected set is derived
// from the awscloud constant source, never from the runtime it guards, so the
// guard is not tautological.
//
// What the guard does NOT catch: a fully data-dependent target_type that a
// scanner builds but no test ever feeds through the runtime layer, and a
// name-vs-ARN mismatch on an edge that sets neither target_arn nor an
// ARN-shaped target_resource_id (a documented name-keyed-with-correlation
// edge). Those remain the scanner author's responsibility.
package relguard
