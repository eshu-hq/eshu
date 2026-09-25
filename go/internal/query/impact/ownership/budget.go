// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ownership

// Ownership budget. The owner statements (statements.go) are grant-free, so
// their cost depends on the number of checked keys and the owner rows they
// match, not on the caller's grant size. Measured on the pinned NornicDB build
// (docs/internal/evidence/5167-impact-grant.md), uncached:
//
//	chunk   CloudResource  WorkloadInstance  TerraformStateResource   (2048 keys each)
//	   50        0.247 s           0.241 s                 0.244 s
//	  100        0.359 s           0.388 s                 0.363 s
//	  250        0.788 s           0.845 s                 0.818 s
//	  500        1.502 s           1.592 s                 1.546 s
//
// Per-chunk cost grows with the square of the chunk (a 2000-key statement
// took ~6 s), so ChunkSize is 50. The shipped statements (with ORDER BY and
// LIMIT $row_limit), measured through Checker at grant 8, 128, and 1000, take
// 0.29-0.33 s per 2048 keys per class on NornicDB (0.10-0.35 s on Neo4j 2026):
// ~0.15 ms per key at fan-in ~1, whatever the grant.
//
// MaxCheckedKeys bounds the distinct statement-checked keys one request sends
// across all classes: at fan-in ~1 (one owner per key) that is ~0.7 s on
// NornicDB, and 0.25 s for a capped 6144-key page on Neo4j 2026 community.
//
// RowLimit bounds the rows a chunk returns, not the owner edges the engine
// expands before DISTINCT, ORDER BY, and LIMIT, so a widely shared node (a
// hub: a shared VPC, KMS key, or role) costs its whole fan-in. Measured by
// TestLiveImpactOwnershipHubFanIn with 50 hub CloudResources of 2000 owners
// each, a unique $nonce per run, n=9 per figure:
//
//	backend              one 50-hub chunk          4500-key page holding the 50 hubs
//	Neo4j 2026           0.021-0.036 s median,     0.74-2.0 s median, 2.58 s max
//	                     0.43 s max (cold)
//	NornicDB (pinned)    12.3-14.7 s (n=6)         not measured
//
// On Neo4j the worst case stays inside the 10 s graph-read deadline. On the
// pinned NornicDB build one hub chunk exceeds it, so a hub-heavy page there
// fails closed with a graph-read deadline error, never a partial answer. In
// either case a hub whose granted owner sorts past RowLimit is left unchecked:
// ungranted, and the caller reports truncated.
//
// The cap is at least the largest page any route builds
// (trace-resource-to-code: 201 rows x up to 21 nodes = 4221), so an ordinary
// page is never capped; a larger node set is checked up to the cap in
// first-seen order and the rest are ungranted, and the caller reports
// truncated.
const (
	// ChunkSize is the most keys one ownership statement carries in $uids.
	ChunkSize = 50
	// MaxCheckedKeys is the per-request cap on distinct statement-checked keys.
	MaxCheckedKeys = 4500
	// RowLimit caps one owner statement's rows (distinct key/owner pairs): an
	// average of 16 owning repositories per key in a full chunk. A chunk that
	// hits it leaves its not-yet-admitted keys unchecked (ungranted), and the
	// verdict reports Capped so the caller reports truncated.
	RowLimit = ChunkSize * 16
)

// CheckedKeyCap returns how many distinct statement-checked keys one request
// may check. It is grant-independent (the statements do not read the grant)
// and is MaxCheckedKeys for any non-empty grant, zero for an empty one. Keys
// past it are ungranted and the caller reports truncated. Go-checked classes
// (Repository and the repo_id-carrying labels) cost no statement and are never
// capped.
func CheckedKeyCap(grantSize int) int {
	if grantSize <= 0 {
		return 0
	}
	return MaxCheckedKeys
}
