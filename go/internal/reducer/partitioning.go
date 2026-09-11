// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"

// PartitionHashForKey forwards to [sharedintent.PartitionHashForKey].
func PartitionHashForKey(partitionKey string) uint64 {
	return sharedintent.PartitionHashForKey(partitionKey)
}

// PartitionForKey forwards to [sharedintent.PartitionForKey].
func PartitionForKey(partitionKey string, partitionCount int) (int, error) {
	return sharedintent.PartitionForKey(partitionKey, partitionCount)
}
