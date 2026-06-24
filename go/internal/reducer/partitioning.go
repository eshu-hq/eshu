// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// PartitionHashForKey returns the stable uint64 hash prefix used to assign a
// shared projection key to worker partitions.
func PartitionHashForKey(partitionKey string) uint64 {
	digest := sha256.Sum256([]byte(partitionKey))
	return binary.BigEndian.Uint64(digest[:8])
}

// PartitionForKey returns the stable partition id for one shared projection key.
// Uses SHA256 and takes the first 8 bytes as big-endian uint64 mod partitionCount.
func PartitionForKey(partitionKey string, partitionCount int) (int, error) {
	if partitionCount <= 0 {
		return 0, fmt.Errorf("partitionCount must be positive, got %d", partitionCount)
	}

	return int(PartitionHashForKey(partitionKey) % uint64(partitionCount)), nil
}
