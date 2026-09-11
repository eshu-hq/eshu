// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import worker "github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"

// LatestIntentsByRepoAndPartition forwards to
// [worker.LatestIntentsByRepoAndPartition].
func LatestIntentsByRepoAndPartition(intents []SharedProjectionIntentRow) ([]SharedProjectionIntentRow, []string) {
	return worker.LatestIntentsByRepoAndPartition(intents)
}

// FilterAuthoritativeIntents forwards to [worker.FilterAuthoritativeIntents].
func FilterAuthoritativeIntents(
	intents []SharedProjectionIntentRow,
	acceptedGen AcceptedGenerationLookup,
) (active []SharedProjectionIntentRow, staleIDs []string) {
	return worker.FilterAuthoritativeIntents(intents, acceptedGen)
}
