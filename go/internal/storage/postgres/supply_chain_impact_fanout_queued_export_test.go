// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// FindQueuedSupplyChainImpactHoldersQueryForTest exposes the shipped holder
// prefilter to the external test package, so the live differential and the plan
// proof run the production text instead of a copy (#10972).
const FindQueuedSupplyChainImpactHoldersQueryForTest = findQueuedSupplyChainImpactHoldersQuery

// ReopenSupplyChainImpactHolderQueryForTest exposes the shipped per-holder
// reopen statement, so the differential derives its read-only classification
// from the statement the prefilter must agree with.
const ReopenSupplyChainImpactHolderQueryForTest = reopenSupplyChainImpactHolderQuery

// ReopenSupplyChainImpactMatchedStatusesForTest exposes the status set the
// reopen and the prefilter share.
const ReopenSupplyChainImpactMatchedStatusesForTest = reopenSupplyChainImpactMatchedStatuses
