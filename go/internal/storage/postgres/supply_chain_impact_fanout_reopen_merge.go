// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strconv"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// The #10972 stamp merge the reopen statement writes on a row it does not
// return to pending. Rule: a stamp is never narrowed while a pass that would
// read it has not run. The merged stamp carries the incoming origin's identity
// fields and, as cves, the sorted distinct union of the stored and incoming
// ids in byte order (COLLATE "C", the order sort.Strings gives). The result is
// unbounded when either side is unbounded or the union exceeds
// supplychaincore.FanOutOriginCVEIDCap: the sentinel when the incoming stamp
// names a set, or the incoming stamp itself (no cves key, or the sentinel)
// when it does not. A stored cves array with no usable string id is unbounded
// too: the Go reader (fanOutStampCVEIDs) finds no set there and runs the pass
// full, so a union with the incoming ids alone would narrow it. A row with no
// stamp object has nothing to merge, so it takes the incoming stamp as the
// statement always wrote it.
//
// Every fragment reads work.payload, the row the UPDATE locked and evaluates
// under EvalPlanQual, never the target CTE's snapshot: two fan-outs that reach
// one holder serialize on its row lock, and the second merges into what the
// first committed, so neither loses its ids.

// fanOutStampMergeCapSQL is the union cap, rendered from the Go constant so
// the SQL and CapFanOutStampCVEIDs cannot drift.
var fanOutStampMergeCapSQL = strconv.Itoa(supplychaincore.FanOutOriginCVEIDCap)

// fanOutStampIDsSQL lists the non-empty string ids of a jsonb cves array as
// text rows. Non-string members are dropped, as fanOutStampCVEIDs drops them.
func fanOutStampIDsSQL(cves string) string {
	return `SELECT member #>> '{}' AS id FROM jsonb_array_elements(` + cves + `) AS member
                WHERE jsonb_typeof(member) = 'string' AND member #>> '{}' <> ''`
}

// fanOutMergedStampSQL is the merged stamp for the row's stored stamp and the
// incoming stamp (incomingArg, the jsonb stamp object), its ids (cvesArg, a
// jsonb array), and whether they are bounded (boundedArg). See the rule above.
func fanOutMergedStampSQL(incomingArg, cvesArg, boundedArg string) string {
	stored := fanOutStampSQL + `->'` + supplychaincore.FanOutStampCVEKey + `'`
	sentinel := `jsonb_set(` + incomingArg + `, '{` + supplychaincore.FanOutStampCVEKey + `}', '["` +
		supplychaincore.FanOutStampSentinel + `"]'::jsonb, TRUE)`
	return `CASE
            WHEN NOT ` + boundedArg + ` THEN ` + incomingArg + `
            WHEN jsonb_typeof(` + fanOutStampSQL + `) IS DISTINCT FROM 'object' THEN ` + incomingArg + `
            WHEN ` + fanOutStampUnboundedSQL(fanOutStampSQL) + ` THEN ` + sentinel + `
            WHEN NOT EXISTS (` + fanOutStampIDsSQL(stored) + `) THEN ` + sentinel + `
            ELSE (
                SELECT CASE
                    WHEN count(*) > ` + fanOutStampMergeCapSQL + ` THEN ` + sentinel + `
                    ELSE jsonb_set(` + incomingArg + `, '{` + supplychaincore.FanOutStampCVEKey + `}',
                        COALESCE(jsonb_agg(to_jsonb(merged.id) ORDER BY merged.id COLLATE "C"), '[]'::jsonb), TRUE)
                END
                FROM (` + fanOutStampIDsSQL(stored) + `
                      UNION
                      ` + fanOutStampIDsSQL(cvesArg) + `) AS merged
            )
        END`
}
