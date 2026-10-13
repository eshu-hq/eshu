// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplanestore

// ListCrossplaneRedriveTargetScopesQueryForTest exposes the cross-scope
// target-discovery statement to the external query-plan test, so it EXPLAINs
// the shipped query rather than a hand-frozen copy.
const ListCrossplaneRedriveTargetScopesQueryForTest = listCrossplaneRedriveTargetScopesQuery

// CrossplaneRedriveOutcomeCompletedForTest exposes the sweep's completed
// outcome to the external behavior and catch-up tests, which assert the
// sweep result vocabulary without importing private identifiers.
const CrossplaneRedriveOutcomeCompletedForTest = crossplaneRedriveOutcomeCompleted
