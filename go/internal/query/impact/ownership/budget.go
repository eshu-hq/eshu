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
// 0.29-0.33 s per 2048 keys per class: ~0.15 ms per key, whatever the grant.
// MaxCheckedKeys bounds the distinct statement-checked keys one request sends
// across all classes, so the worst case is ~0.7 s, far inside the 10 s
// graph-read deadline, with headroom for owner fan-in (a shared CloudResource
// returns one row per distinct owning repository, up to RowLimit per chunk).
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
