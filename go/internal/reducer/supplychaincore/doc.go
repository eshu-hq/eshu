// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package supplychaincore holds the shared value types of the reducer
// supply-chain impact family: the impact finding, its provenance, priority,
// reachability, detection-profile, remediation, and suppression data types.
//
// It exists to break a real import cycle inside the reducer supply-chain
// family (issue #6061). The impact-finding half of the family uses these
// types as it composes findings, and the suppression half both takes a
// [SupplyChainImpactFinding] as input and contributes the
// [SupplyChainSuppressionDecision] embedded on every finding. Splitting those
// halves into sibling packages while each declared the other's types would
// make them import each other. Hoisting the shared types into this leaf lets
// both halves depend on it instead, so neither depends on the other.
//
// The package is deliberately behavior-free: it declares data types, their
// string-enum constants, and nothing else. It imports only the standard
// library, never internal/reducer or any sibling, so it can never re-form the
// cycle it was created to break. Every function that reads or builds these
// types -- evaluation, provenance selection, priority scoring, reachability
// enrichment, remediation, and the writer -- lives in internal/reducer and
// keeps its existing spelling through the alias shims declared there.
package supplychaincore
