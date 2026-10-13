// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import "unicode"

// patternHasTrigram reports whether pg_trgm can extract at least one
// trigram from the ILIKE pattern the walk binds: the pattern must contain a
// run of at least three alphanumeric characters. Wildcards (%, _), the
// escape character, and every other non-alphanumeric rune break a run,
// mirroring how pg_trgm splits a pattern into words before extracting
// trigrams. The check is deliberately conservative: it treats an escaped
// letter as a break even though ILIKE reads it as a literal, so it can only
// skip a tail that might have worked, never run one that cannot. Skipping
// is safe because the key-ordered phases are exact on their own; running a
// tail with no extractable trigram is not bounded — the planner falls back
// to a sequential scan (measured at 3,930 ms warm for `db_` on the QA environment
// read replica against a 400 ms tail cap, #8002).
func patternHasTrigram(pattern string) bool {
	run := 0
	for _, r := range pattern {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			run++
			if run >= 3 {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}
