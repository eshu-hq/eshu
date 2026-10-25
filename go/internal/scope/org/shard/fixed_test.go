// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shard

import (
	"fmt"
	"math/rand"
	"testing"
)

// The shim behind the #9019 ruling measured the trie on random allowlists
// (seed 9019, shardshim.go): K and the fill range per N. These rows are fixed
// expectations: the hash and the split rule are durable identity, so a change
// that moves any of them renames shard scopes and needs a migration.
var shimRows = map[int]struct {
	k, low, high int
}{
	100:  {1, 100, 100},
	101:  {2, 50, 51},
	150:  {2, 68, 82},
	200:  {3, 52, 94},
	500:  {8, 51, 75},
	999:  {16, 48, 75},
	4985: {64, 63, 96},
}

// shimAllowlists replays the shim's random stream: per N it draws N names,
// drops duplicates, then consumes the 4 x 300 add/remove draws the shim made
// before the next N.
func shimAllowlists() map[int][]string {
	rng := rand.New(rand.NewSource(9019))
	out := map[int][]string{}
	for _, n := range []int{100, 101, 150, 200, 500, 1000, 5000} {
		seen := map[string]bool{}
		repos := make([]string, 0, n)
		for range n {
			repository := fmt.Sprintf("acme/repo-%06d", rng.Intn(1_000_000))
			if !seen[repository] {
				seen[repository] = true
				repos = append(repos, repository)
			}
		}
		for range 4 * 300 {
			rng.Intn(1_000_000)
			rng.Intn(len(repos))
		}
		out[len(repos)] = repos
	}
	return out
}

func TestPartitionMatchesTheShimRows(t *testing.T) {
	t.Parallel()

	allowlists := shimAllowlists()
	for n, want := range shimRows {
		allowed, ok := allowlists[n]
		if !ok {
			t.Fatalf("no shim allowlist of %d repositories", n)
		}
		shards := Partition(testBase, "acme", allowed)
		assertPartition(t, shards, "acme", allowed)
		low, high := n, 0
		for _, shard := range shards {
			low = min(low, len(shard.Repositories))
			high = max(high, len(shard.Repositories))
		}
		if len(shards) != want.k || low != want.low || high != want.high {
			t.Fatalf("N=%d: K=%d fill=[%d,%d], want K=%d fill=[%d,%d]", n, len(shards), low, high, want.k, want.low, want.high)
		}
	}
}
