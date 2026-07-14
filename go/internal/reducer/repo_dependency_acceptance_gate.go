// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "context"

// RepoDependencyAcceptanceUnitGateKey identifies one leased repository shard
// owner entering the source-repository graph replacement critical section.
type RepoDependencyAcceptanceUnitGateKey struct {
	Domain           string
	AcceptanceUnitID string
	PartitionID      int
	PartitionCount   int
	LeaseOwner       string
}

// RepoDependencyAcceptanceUnitGate serializes only conflicting replacement
// work for one source repository while leaving distinct repositories parallel.
// The callback receives a transaction-bound intent reader so completion commits
// with the Postgres lock that guards the graph replacement.
type RepoDependencyAcceptanceUnitGate interface {
	WithAcceptanceUnit(
		ctx context.Context,
		key RepoDependencyAcceptanceUnitGateKey,
		fn func(context.Context, RepoDependencyProjectionIntentReader) error,
	) (bool, error)
}
