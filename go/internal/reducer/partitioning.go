package reducer

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// PartitionForKey returns the stable partition id for one shared projection key.
// Uses SHA256 and takes the first 8 bytes as big-endian uint64 mod partitionCount.
func PartitionForKey(partitionKey string, partitionCount int) (int, error) {
	if partitionCount <= 0 {
		return 0, fmt.Errorf("partitionCount must be positive, got %d", partitionCount)
	}

	digest := sha256.Sum256([]byte(partitionKey))
	val := binary.BigEndian.Uint64(digest[:8])

	return int(val % uint64(partitionCount)), nil
}
