// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shard

import (
	"crypto/sha256"
	"encoding/binary"
	"slices"
	"strings"
)

const (
	// MaxRepositories is the most allowlisted repositories one shard
	// holds. It equals the org fan-out cap of one claim, so a shard's
	// per-repository reads always fit one claim. A node holding more splits.
	MaxRepositories = 100
	// MaxDepth caps the split depth. A leaf at this depth that still holds
	// more than MaxRepositories (more than 100 repositories sharing a
	// 32-bit hash prefix) stays over the cap and is flagged OverCap; the
	// collector's per-claim fan-out cap is the backstop for it.
	MaxDepth = 32
)

// Leaf is one shard of an organization target's partition.
type Leaf struct {
	// ScopeID is the shard's ingestion scope id: the operator's base scope_id
	// when the target has one shard, otherwise ScopeID(base, Bits).
	ScopeID string
	// Bits is the leaf's hash-bit prefix, "" for the unsplit root.
	Bits string
	// Repositories are the shard's normalized same-org repositories in
	// allowlist order.
	Repositories []string
	// OverCap reports a leaf at MaxDepth still holding more than
	// MaxRepositories.
	OverCap bool
}

// Partition splits the same-org repositories of an organization target into
// shards of at most MaxRepositories. It is a pure function of its inputs:
// the shard set never depends on allowlist order, earlier partitions, or any
// stored state. Repositories are normalized with NormalizeRepository;
// duplicates, case variants, malformed entries, and repositories of another
// owner are dropped (an org stream never carries another owner's repository).
//
// The partition is a binary trie over a repository's hash (the first 8 bytes
// of sha256 of its normalized owner/name, read most significant bit first): a
// node holding more than MaxRepositories splits on its next bit, down to
// MaxDepth. Empty leaves are dropped. A target with one shard keeps the base
// scope id unchanged, so every target of at most 100 same-org repositories
// keeps the scope it had before sharding existed; a target with more names
// every leaf ScopeID(base, bits) with its full bit prefix. The result is
// ordered by bit prefix and always holds at least one shard.
func Partition(baseScopeID, organization string, allowedRepositories []string) []Leaf {
	return partitionAtDepth(baseScopeID, organization, allowedRepositories, MaxDepth)
}

type hashedRepository struct {
	name string
	hash uint64
	// position is the repository's index in the deduplicated allowlist.
	position int
}

func partitionAtDepth(baseScopeID, organization string, allowedRepositories []string, maxDepth int) []Leaf {
	baseScopeID = strings.TrimSpace(baseScopeID)
	repositories := sameOrganizationRepositories(organization, allowedRepositories)
	var leaves []Leaf
	var split func(bits string, depth int, members []hashedRepository)
	split = func(bits string, depth int, members []hashedRepository) {
		if len(members) == 0 {
			return
		}
		if len(members) <= MaxRepositories || depth >= maxDepth {
			leaves = append(leaves, Leaf{
				Bits:         bits,
				Repositories: names(members),
				OverCap:      len(members) > MaxRepositories,
			})
			return
		}
		var zero, one []hashedRepository
		for _, member := range members {
			if member.hash>>(63-uint(depth))&1 == 0 {
				zero = append(zero, member)
			} else {
				one = append(one, member)
			}
		}
		split(bits+"0", depth+1, zero)
		split(bits+"1", depth+1, one)
	}
	split("", 0, repositories)
	if len(leaves) == 0 {
		return []Leaf{{ScopeID: baseScopeID, Repositories: []string{}}}
	}
	if len(leaves) == 1 {
		leaves[0].ScopeID = baseScopeID
		return leaves
	}
	for i := range leaves {
		leaves[i].ScopeID = ScopeID(baseScopeID, leaves[i].Bits)
	}
	return leaves
}

// sameOrganizationRepositories returns the distinct normalized repositories of
// organization, in allowlist order, with their hashes.
func sameOrganizationRepositories(organization string, allowed []string) []hashedRepository {
	prefix := NormalizeOrganization(organization) + "/"
	seen := make(map[string]struct{}, len(allowed))
	out := make([]hashedRepository, 0, len(allowed))
	for _, raw := range allowed {
		repository := NormalizeRepository(raw)
		if repository == "" || !strings.HasPrefix(repository, prefix) {
			continue
		}
		if _, ok := seen[repository]; ok {
			continue
		}
		seen[repository] = struct{}{}
		out = append(out, hashedRepository{name: repository, hash: repositoryHash(repository), position: len(out)})
	}
	return out
}

func names(members []hashedRepository) []string {
	ordered := slices.Clone(members)
	slices.SortFunc(ordered, func(a, b hashedRepository) int { return a.position - b.position })
	out := make([]string, 0, len(ordered))
	for _, member := range ordered {
		out = append(out, member.name)
	}
	return out
}

// repositoryHash is the partition key of a normalized repository: the first 8
// bytes of its sha256, big endian. It is durable identity: changing it renames
// every shard scope.
func repositoryHash(repository string) uint64 {
	sum := sha256.Sum256([]byte(repository))
	return binary.BigEndian.Uint64(sum[:8])
}

// For returns the shard holding repository (normalized here), or false
// when no shard holds it.
func For(shards []Leaf, repository string) (Leaf, bool) {
	repository = NormalizeRepository(repository)
	if repository == "" {
		return Leaf{}, false
	}
	for _, shard := range shards {
		if slices.Contains(shard.Repositories, repository) {
			return shard, true
		}
	}
	return Leaf{}, false
}
