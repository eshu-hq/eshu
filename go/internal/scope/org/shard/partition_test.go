// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shard

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

const testBase = "security-alert:github-org:acme"

func testRepositories(n int) []string {
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, fmt.Sprintf("acme/service-%04d", i))
	}
	return out
}

// assertPartition checks the acceptance criteria every partition must meet:
// exactly one shard per distinct same-org repository, at most
// MaxRepositories per shard unless the depth cap is reached, unique ids
// that extend the base by a bounded token, and no empty shard.
func assertPartition(t *testing.T, shards []Leaf, organization string, allowed []string) {
	t.Helper()
	want := map[string]bool{}
	for _, repository := range allowed {
		if normalized := NormalizeRepository(repository); normalized != "" && strings.HasPrefix(normalized, organization+"/") {
			want[normalized] = true
		}
	}
	seen := map[string]string{}
	ids := map[string]bool{}
	for _, shard := range shards {
		if ids[shard.ScopeID] {
			t.Fatalf("duplicate shard id %q", shard.ScopeID)
		}
		ids[shard.ScopeID] = true
		if !strings.HasPrefix(shard.ScopeID, testBase) || len(shard.ScopeID)-len(testBase) > MaxSuffixLength {
			t.Fatalf("shard id %q does not extend %q by at most %d bytes", shard.ScopeID, testBase, MaxSuffixLength)
		}
		if len(shards) > 1 && len(shard.Repositories) == 0 {
			t.Fatalf("shard %q is empty", shard.ScopeID)
		}
		if len(shard.Repositories) > MaxRepositories && !shard.OverCap {
			t.Fatalf("shard %q holds %d repositories without the depth-cap flag", shard.ScopeID, len(shard.Repositories))
		}
		for _, repository := range shard.Repositories {
			if other, ok := seen[repository]; ok {
				t.Fatalf("repository %q is in shards %q and %q", repository, other, shard.ScopeID)
			}
			seen[repository] = shard.ScopeID
			if !want[repository] {
				t.Fatalf("shard %q holds %q, which is not a distinct same-org allowlisted repository", shard.ScopeID, repository)
			}
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("partition places %d repositories, want %d", len(seen), len(want))
	}
}

func shardIDs(shards []Leaf) []string {
	out := make([]string, 0, len(shards))
	for _, shard := range shards {
		out = append(out, shard.ScopeID)
	}
	return out
}

func TestPartitionKeepsTheBaseScopeAtTheCap(t *testing.T) {
	t.Parallel()

	shards := Partition(testBase, "acme", testRepositories(MaxRepositories))
	if got := shardIDs(shards); !slices.Equal(got, []string{testBase}) {
		t.Fatalf("shard ids = %v, want only the base scope", got)
	}
	if len(shards[0].Repositories) != MaxRepositories {
		t.Fatalf("base shard holds %d repositories, want %d", len(shards[0].Repositories), MaxRepositories)
	}
}

func TestPartitionSplitsOneOverTheCapIntoFullDepthNames(t *testing.T) {
	t.Parallel()

	allowed := testRepositories(MaxRepositories + 1)
	shards := Partition(testBase, "acme", allowed)
	assertPartition(t, shards, "acme", allowed)
	if len(shards) < 2 {
		t.Fatalf("101 repositories gave %v, want at least 2 shards", shardIDs(shards))
	}
	for _, shard := range shards {
		base, bits, ok := ParseScopeID(shard.ScopeID)
		if !ok || base != testBase || bits == "" || bits != shard.Bits {
			t.Fatalf("shard id %q parses to (%q, %q, %v), want base %q and bits %q", shard.ScopeID, base, bits, ok, testBase, shard.Bits)
		}
	}
}

func TestPartitionEmptyAndForeignAllowlistsKeepTheBase(t *testing.T) {
	t.Parallel()

	for name, allowed := range map[string][]string{
		"empty":   nil,
		"foreign": {"other/a", "other/b"},
		"invalid": {"no-slash", "/", "a/b/c"},
	} {
		shards := Partition(testBase, "acme", allowed)
		if got := shardIDs(shards); !slices.Equal(got, []string{testBase}) {
			t.Fatalf("%s: shard ids = %v, want only the base scope", name, got)
		}
		if len(shards[0].Repositories) != 0 {
			t.Fatalf("%s: base shard holds %v, want none", name, shards[0].Repositories)
		}
	}
}

func TestPartitionCountsCaseVariantsAndDuplicatesOnce(t *testing.T) {
	t.Parallel()

	allowed := testRepositories(MaxRepositories)
	padded := append(slices.Clone(allowed), strings.ToUpper(allowed[0]), " "+allowed[1]+".git ", "/"+allowed[2]+"/", "Other/x")
	if got := shardIDs(Partition(testBase, " ACME/ ", padded)); !slices.Equal(got, []string{testBase}) {
		t.Fatalf("case variants and duplicates split the target: %v", got)
	}
}

func TestPartitionKeepsAllowlistOrderInsideALeaf(t *testing.T) {
	t.Parallel()

	allowed := []string{"acme/zeta", "acme/alpha", "other/x", "acme/mid"}
	shards := Partition(testBase, "acme", allowed)
	if got := shards[0].Repositories; !slices.Equal(got, []string{"acme/zeta", "acme/alpha", "acme/mid"}) {
		t.Fatalf("repositories = %v, want same-org allowlist order", got)
	}
}

func TestPartitionIsIndependentOfAllowlistOrder(t *testing.T) {
	t.Parallel()

	allowed := testRepositories(737)
	first := Partition(testBase, "acme", allowed)
	shuffled := slices.Clone(allowed)
	rand.New(rand.NewSource(9019)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	second := Partition(testBase, "acme", shuffled)
	if !slices.Equal(shardIDs(first), shardIDs(second)) {
		t.Fatalf("shard ids changed with order: %v vs %v", shardIDs(first), shardIDs(second))
	}
	for i := range first {
		a, b := slices.Sorted(slices.Values(first[i].Repositories)), slices.Sorted(slices.Values(second[i].Repositories))
		if !slices.Equal(a, b) {
			t.Fatalf("shard %q membership changed with order", first[i].ScopeID)
		}
	}
}

func TestPartitionOneAddRetiresAtMostOneShardAndAddsAtMostTwo(t *testing.T) {
	t.Parallel()

	for _, n := range []int{100, 101, 250, 999} {
		allowed := testRepositories(n)
		before := Partition(testBase, "acme", allowed)
		for i := range 40 {
			after := Partition(testBase, "acme", append(slices.Clone(allowed), fmt.Sprintf("acme/added-%02d", i)))
			removed, added := diff(shardIDs(before), shardIDs(after))
			if len(removed) > 1 || len(added) > 2 {
				t.Fatalf("N=%d add %d: retired %v added %v, want at most 1 and 2", n, i, removed, added)
			}
		}
	}
}

func diff(before, after []string) (removed, added []string) {
	for _, id := range before {
		if !slices.Contains(after, id) {
			removed = append(removed, id)
		}
	}
	for _, id := range after {
		if !slices.Contains(before, id) {
			added = append(added, id)
		}
	}
	return removed, added
}

func TestPartitionDepthCapFlagsAnOverfullLeaf(t *testing.T) {
	t.Parallel()

	// 101 repositories whose hashes share their first 32 bits cannot split
	// below the cap: the partition keeps one over-cap leaf as the backstop.
	allowed := sameHashPrefixRepositories(t, MaxRepositories+1)
	shards := partitionAtDepth(testBase, "acme", allowed, 2)
	assertPartition(t, shards, "acme", allowed)
	over := 0
	for _, shard := range shards {
		if shard.OverCap {
			over++
			if len(shard.Repositories) <= MaxRepositories {
				t.Fatalf("over-cap shard %q holds %d", shard.ScopeID, len(shard.Repositories))
			}
		}
	}
	if over != 1 {
		t.Fatalf("over-cap shards = %d, want 1 (%v)", over, shardIDs(shards))
	}
}

// sameHashPrefixRepositories finds n repositories whose hashes share their
// first two bits, so a depth cap of 2 leaves them in one leaf.
func sameHashPrefixRepositories(t *testing.T, n int) []string {
	t.Helper()
	var out []string
	for i := 0; len(out) < n; i++ {
		repository := fmt.Sprintf("acme/r-%06d", i)
		if repositoryHash(repository)>>62 == 0 {
			out = append(out, repository)
		}
		if i > 1_000_000 {
			t.Fatal("could not find repositories with a shared hash prefix")
		}
	}
	return out
}
