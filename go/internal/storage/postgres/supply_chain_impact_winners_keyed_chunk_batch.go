// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"strings"
)

// insertWinnersKeyedChunkIdentPrefix heads the single batched INSERT that
// records the chunk's identifiers (#10839): one statement carries the whole
// chunk as multi-row VALUES, so a saturated tick spends one round trip here
// instead of one per identifier. The chunk is at most a few hundred rows of
// five short columns, far under the Postgres parameter limit. The per-row
// statement is gone; callers build the batch through
// winnersKeyedChunkIdentBatch.
const insertWinnersKeyedChunkIdentPrefix = `INSERT INTO ` + winnersKeyedChunkIdentsTable + ` VALUES `

// winnersKeyedChunkIdent is one chunk identifier's five columns.
type winnersKeyedChunkIdent struct{ pkg, repo, digest, c0, advisory string }

// winnersKeyedChunkIdentBatch packs identifiers into one multi-row INSERT
// over the chunk temp table, with Postgres $n placeholders in row-major
// order. An empty input is a caller bug: the chunk always inserts at least
// the first identifier.
func winnersKeyedChunkIdentBatch(idents []winnersKeyedChunkIdent) (string, []any) {
	var query strings.Builder
	query.WriteString(insertWinnersKeyedChunkIdentPrefix)
	args := make([]any, 0, len(idents)*5)
	for i, id := range idents {
		if i > 0 {
			query.WriteByte(',')
		}
		base := i*5 + 1
		fmt.Fprintf(&query, "($%d,$%d,$%d,$%d,$%d)", base, base+1, base+2, base+3, base+4)
		args = append(args, id.pkg, id.repo, id.digest, id.c0, id.advisory)
	}
	return query.String(), args
}
