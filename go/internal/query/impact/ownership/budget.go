// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ownership

// Ownership budget. The owner statements (statements.go) are grant-free, so
// their cost depends on the number of checked keys and their owner fan-in, not
// on the caller's grant size. Live measurements run on Neo4j
// (neo4j:2026-community), the owner-mandated test backend since 2026-09-25;
// see docs/internal/evidence/5167-impact-grant.md.
//
// Measured on Neo4j, through Checker, uncached (a unique $nonce per run):
//
//   - fan-in ~1: 0.10-0.35 s per 2048 keys per class at grant 8, 128, and
//     1000, 0 misjudged keys; a 6144-key page capped at MaxCheckedKeys takes
//     0.252 s.
//   - hub fan-in (TestLiveImpactOwnershipHubFanIn: 50 CloudResources of 2000
//     owners each, n=9): one 50-hub chunk, median 0.036 s, max 0.428 s (cold);
//     a 4500-key page holding the 50 hubs, median 0.736 s, max 1.359 s (with
//     the whole live suite's fixtures loaded: median 2.000 s, max 2.576 s).
//
// So the worst case measured stays inside the 10 s per-statement graph-read
// deadline. RowLimit bounds the rows a chunk returns, not the owner edges the
// engine expands before DISTINCT, ORDER BY, and LIMIT, so a hub costs its whole
// fan-in; a hub whose granted owner sorts past RowLimit is left unchecked
// (ungranted, and the caller reports truncated), never admitted.
//
// Historical, NornicDB only (measured on the pinned NornicDB build before the
// 2026-09-25 Neo4j rule; not re-measured): the chunk sweep that set ChunkSize
// (per-chunk cost grows with the square of the chunk; a 2000-key statement
// took ~6 s) was
//
//	chunk   CloudResource  WorkloadInstance  TerraformStateResource   (2048 keys each)
//	   50        0.247 s           0.241 s                 0.244 s
//	  100        0.359 s           0.388 s                 0.363 s
//	  250        0.788 s           0.845 s                 0.818 s
//	  500        1.502 s           1.592 s                 1.546 s
//
// the shipped statements took 0.29-0.33 s per 2048 keys per class (~0.7 s at
// the cap, fan-in ~1), and one 50-hub chunk took 12.3-14.7 s (n=6), over the
// 10 s deadline, so on that backend a hub-heavy page fails closed with a
// graph-read deadline error and admits nothing.
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
