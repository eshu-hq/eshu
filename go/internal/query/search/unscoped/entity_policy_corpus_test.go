// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped_test

import (
	"sync"
	"time"
)

// The entity policy corpus is a key space shaped like the content_entities
// measurements of the #7009 S1 shim (docs/internal/evidence/7009-unscoped-entity-search-figures.md),
// scaled down so the fake stays small. Positions and costs are synthetic; no
// row of a real corpus is in it.
const (
	// entityPolicyRows is the length of the synthetic key space. The ordered
	// phase of one default call covers 36,000 rows, so every position past
	// that is reached only by the trigram tail or by a resumed call.
	entityPolicyRows = 100_000
	// entityRowCost is the walk cost of one row. The S1 ordered-window fit is 0.3 ms
	// per statement plus 0.34 us per row plus 10.2 ns per byte (a 1,000-row
	// probe is about 5 ms and a 5,000-row step 18-43 ms); 400 ms then buys
	// about 46,000-106,000 rows. 11.25 us per row gives 36,000 rows in the
	// 0.20 x budget phase one at the 2000 ms default (#9306 ruling 2026-10-29:
	// scaled x2.5 with the cap so the ordered-phase geometry is unchanged),
	// under the fit range so the fake never lets the walk off easier than QA.
	entityRowCost = 11250 * time.Nanosecond
	// entityLargeValueRow is the 0-based key position of the synthetic
	// 13.1 MB value. S1 found the five values over 1 MB at the end
	// of the key order, where only the tail meets them.
	entityLargeValueRow = 99_000
)

var (
	entityTemplateOnce sync.Once
	entityTemplate     []fakeEntityRow
)

// entityPolicyCorpus builds the policy key space with the matches at the
// 1-based positions given and the tail wall tailCost. Rows are copied from one
// shared template so a class costs a slice copy, not a fresh build.
func entityPolicyCorpus(tailCost time.Duration, positions ...int) *fakeCorpus {
	entityTemplateOnce.Do(func() {
		entityTemplate = newEntityCorpus(entityPolicyRows, entityRowCost, func(int) bool { return false }).entities
	})
	corpus := &fakeCorpus{
		entities: append([]fakeEntityRow(nil), entityTemplate...),
		tailCost: func(int) time.Duration { return tailCost },
		// The S1 model has a cancelled tail end at its timeout plus 3 ms.
		tailLag: 3 * ms,
	}
	for _, p := range positions {
		corpus.entities[p-1].match = true
	}
	return corpus
}

// withEntityLargeValue puts the 13.1 MB value into the tail's candidates.
// S1 tail finding: a tail cancel whose timeout fires between about 30 and
// 170 ms of tail time waits for that value's recheck and lands at about
// 170 ms; outside that window the lag stays small.
func withEntityLargeValue(corpus *fakeCorpus) *fakeCorpus {
	corpus.tailDoc = &fakeTailDoc{row: entityLargeValueRow, start: 30 * ms, end: 170 * ms}
	return corpus
}

// withEntityProbeCost makes the first rows rows cost total in all, so a call
// reaches the tail with less of a small budget left.
func withEntityProbeCost(corpus *fakeCorpus, rows int, total time.Duration) *fakeCorpus {
	each := total / time.Duration(rows)
	for i := 0; i < rows; i++ {
		corpus.entities[i].cost = each
	}
	return corpus
}

// anchor is one measured (n-th match, key position) pair.
type anchor struct{ match, position int }

// interpolate returns the 1-based positions of matches 1..last, linear between
// the anchors, which must start at match 1 and rise in both fields.
func interpolate(anchors ...anchor) []int {
	var out []int
	for i := 0; i+1 < len(anchors); i++ {
		from, to := anchors[i], anchors[i+1]
		for n := from.match; n < to.match; n++ {
			out = append(out, from.position+(n-from.match)*(to.position-from.position)/(to.match-from.match))
		}
	}
	return append(out, anchors[len(anchors)-1].position)
}

// spaced returns count positions from first, step apart.
func spaced(first, step, count int) []int {
	out := make([]int, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, first+i*step)
	}
	return out
}

// decodePositions are the S1 match-position anchors of the common dense class `decode`
// (1st 41, 11th 1,091, 51st 17,434, 101st 32,046, 151st 36,140, 201st
// 50,401), with later matches so a page past the 201st has a look-ahead.
// Matches up to the 149th lie inside the 36,000 rows of one default ordered
// phase.
func decodePositions() []int {
	return interpolate(
		anchor{1, 41}, anchor{2, 66}, anchor{6, 983}, anchor{11, 1_091}, anchor{21, 10_367},
		anchor{51, 17_434}, anchor{101, 32_046}, anchor{151, 36_140}, anchor{201, 50_401}, anchor{260, 95_000},
	)
}

// Position lists of the other measured classes (S1). Positions past the
// ordered phase stand in for the measured ones, which lie past this key
// space; only their order relative to the ordered phase matters.
var (
	// "a": 11th match at row 13, 201st at row 238.
	aPositions = interpolate(anchor{1, 1}, anchor{11, 13}, anchor{51, 57}, anchor{101, 118}, anchor{151, 188}, anchor{201, 238}, anchor{230, 300})
	// "db_": 11th match at row 231, 201st at row 2,933.
	dbPositions = interpolate(anchor{1, 21}, anchor{11, 231}, anchor{51, 780}, anchor{101, 1_329}, anchor{151, 2_006}, anchor{201, 2_933}, anchor{230, 4_000})
	// "process_entries": two matches, both inside the ordered phase; only the
	// tail proves there is no third.
	processEntriesPositions = []int{9_482, 9_484}
	// "FeaturesTab": four matches, all past the ordered phase.
	featuresTabPositions = []int{65_932, 65_933, 66_167, 66_168}
	// "from_event": 67 matches, all past the ordered phase; the 11th is at
	// 49,000, so a resume chain reaches it on its second call.
	fromEventPositions = spaced(40_000, 900, 67)
	// "getApplicationContext" (the selective long identifier): 69 matches,
	// all past the ordered phase.
	selectivePositions = spaced(40_000, 800, 69)
	// "showImage": 85 matches, all past the ordered phase.
	showImagePositions = spaced(37_000, 700, 85)
	// "checkPrerequisites": 1st at 1,673, 2nd at 11,098, the rest past the
	// ordered phase.
	checkPrerequisitesPositions = append([]int{1_673, 11_098}, spaced(40_000, 1_200, 45)...)
	// "createNewVersion": 1st at 1,673, the rest past the ordered phase.
	createNewVersionPositions = append([]int{1_673}, spaced(38_000, 700, 85)...)
	// SYN half-early-rare: 6 of 11 matches in the first 3,000 rows, 5 past
	// the ordered phase.
	halfEarlyRarePositions = []int{300, 800, 1_200, 1_900, 2_400, 3_000, 60_000, 65_000, 70_000, 75_000, 80_000}
)
