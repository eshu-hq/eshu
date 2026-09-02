// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package iampolicy owns the shared vocabulary for evaluating decoded
// aws_iam_permission statements: the statement and grant shapes, the
// conservative action matchers, and the target-resolution outcome.
//
// A [PrincipalGrant] is one principal's conservatively-trusted effective
// grant. [PrincipalGrant.Allows] and [PrincipalGrant.Denied] honour only the
// two unambiguous wildcard shapes, "*" and "service:*"; a partial wildcard
// such as "iam:Create*" is deliberately not expanded, because an
// over-approximated grant becomes a graph edge asserting access that may not
// exist. [PrincipalGrant.StatementsCovering] resolves a carrier action back to
// every statement that actually grants it, including through a wildcard, so a
// target resolver reads the right resources rather than missing a
// wildcard-granted statement.
//
// [TargetStatus] is the resolution ladder's outcome: exactly one scanned node
// ([TargetResolved]), a wildcard or many matches ([TargetAmbiguous]), or zero
// ([TargetUnresolved]). Ambiguous and unresolved are distinct on purpose —
// they are different operator stories and are counted under different skip
// reasons.
//
// # Why this is a shared leaf
//
// The privilege-escalation slice at the reducer root and the [iamcan] family
// evaluate the same decoded statements against different catalogs and tally
// into different counters, but they share these shapes and matchers. A family
// package may never import the reducer root, so the shared half lives below
// both. The root keeps its spelling through aliases and forwarders in
// iam_permission_grant_compat.go.
//
// This package holds plain data and pure functions. It imports only the
// standard library and the factschema SDK, and it must never import the
// reducer root.
//
// # Observability
//
// This package registers no instrument and performs no I/O. Every refusal it
// classifies is counted by the caller, against that caller's own skip counter.
package iampolicy
