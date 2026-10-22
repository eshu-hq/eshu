// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"fmt"
	"sort"
	"strings"
)

// whyMaxRunes bounds the quoted phrase in a Why line.
const whyMaxRunes = 160

// why builds the one-line reason for a match from the tool's own text: the
// intent phrase or description sentence with the highest summed idf of matched
// query terms (an intent wins a tie, then the earlier unit). When only the name
// matched, it quotes the name. It never generates text.
func why(d *doc, terms []string, idf []float64) string {
	best, bestScore := -1, 0.0
	for i, u := range d.units {
		var s float64
		for ti, term := range terms {
			if _, ok := u.terms[term]; ok {
				s += idf[ti]
			}
		}
		if s > bestScore {
			best, bestScore = i, s
		}
	}
	if best >= 0 {
		u := d.units[best]
		label := "description"
		if u.kind == unitIntent {
			label = "intent"
		}
		return fmt.Sprintf("matches %s \"%s\"", label, oneLine(u.text, whyMaxRunes))
	}
	var hit []string
	for _, term := range terms {
		if _, ok := d.nameTerms[term]; ok {
			hit = append(hit, term)
		}
	}
	sort.Strings(hit)
	return fmt.Sprintf("matches tool name \"%s\" (%s)", oneLine(d.name, whyMaxRunes), strings.Join(hit, ", "))
}
