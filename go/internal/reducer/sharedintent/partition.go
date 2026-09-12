// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sharedintent

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

// PartitionForKey returns the stable partition id for one shared projection
// key. Uses SHA256 and takes the first 8 bytes as big-endian uint64 mod
// partitionCount.
func PartitionForKey(partitionKey string, partitionCount int) (int, error) {
	if partitionCount <= 0 {
		return 0, fmt.Errorf("partitionCount must be positive, got %d", partitionCount)
	}

	return int(PartitionHashForKey(partitionKey) % uint64(partitionCount)), nil // #nosec G115 -- bounded: result is % partitionCount which is range-checked > 0 above, so the value fits int
}

// RowsForPartition returns intent rows whose partition key belongs to one
// worker partition.
func RowsForPartition(rows []Row, partitionID, partitionCount int) []Row {
	var result []Row
	for _, row := range rows {
		p, err := PartitionForKey(row.PartitionKey, partitionCount)
		if err != nil {
			continue
		}
		if p == partitionID {
			result = append(result, row)
		}
	}
	return result
}
