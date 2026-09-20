// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

// bulkGraphSeedBatchSize bounds how many anonymous infra nodes one CREATE
// statement writes. Measured on NornicDB v1.3.3 with UNWIND $rows over
// unconstrained labels: 2,000 rows took 0.14s, 10,000 took 0.51s and 20,000
// took 0.98s; a single 50,000-row statement fails with "Txn is too big to fit
// into one request". 10,000 keeps a 2x margin under the last size that worked.
const bulkGraphSeedBatchSize = 10000

// idRange is an inclusive [First, Last] span of node indexes.
type idRange struct {
	First int
	Last  int
}

// bulkNodeRanges splits indexes 0..total-1 into contiguous inclusive ranges of
// at most size indexes. The bounds are computed here, not inside Cypher:
// NornicDB evaluates `range(0, $count - 1)` with a parameter expression as
// the bound to a single element, which left every seeded label with one node.
// A non-positive total yields no ranges; a non-positive size yields one range
// covering everything.
func bulkNodeRanges(total, size int) []idRange {
	if total <= 0 {
		return nil
	}
	if size <= 0 {
		return []idRange{{First: 0, Last: total - 1}}
	}
	ranges := make([]idRange, 0, (total+size-1)/size)
	for first := 0; first < total; first += size {
		last := first + size - 1
		if last > total-1 {
			last = total - 1
		}
		ranges = append(ranges, idRange{First: first, Last: last})
	}
	return ranges
}
