// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"math"
	"slices"
	"strconv"
	"strings"
)

// scoreDigits is the number of significant digits a score keeps before it
// orders rows. Raw score bits differ by up to 4 ulp between arm64 and amd64
// (fused multiply-add, #7746); 8 digits absorb that noise and keep every
// distinct Reciprocal Rank Fusion score apart (they differ by about 1e-6 at
// the 200-row window).
const scoreDigits = 8

// Quantize renders score to scoreDigits significant digits and parses it
// back. It reports false for NaN and infinities, which never order a row.
func Quantize(score float64) (float64, bool) {
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return 0, false
	}
	quantized, err := strconv.ParseFloat(strconv.FormatFloat(score, 'g', scoreDigits, 64), 64)
	if err != nil {
		return 0, false
	}
	return quantized, true
}

// Scored reports whether id has a finite score in scores, the one
// definition of a scored row: Order ranks exactly these rows, and only these
// rows carry search_backend=hybrid. A NaN or infinite score is unscored.
func Scored(scores map[string]float64, id string) bool {
	raw, ok := scores[id]
	if !ok {
		return false
	}
	_, finite := Quantize(raw)
	return finite
}

// AnyScored reports whether at least one id in scores is Scored. A route
// picks OrderHybrid only when it is true; a window with no finite score is an
// OrderLexical walk.
func AnyScored(scores map[string]float64) bool {
	for id := range scores {
		if Scored(scores, id) {
			return true
		}
	}
	return false
}

// Order returns ids in walk order: rows with a finite score first, by
// quantized score descending, then id ascending; rows without a score after
// them, by id ascending. The input slice is not modified.
func Order(ids []string, scores map[string]float64) []string {
	type keyed struct {
		id     string
		score  float64
		scored bool
	}
	rows := make([]keyed, 0, len(ids))
	for _, id := range ids {
		row := keyed{id: id}
		if raw, ok := scores[id]; ok {
			row.score, row.scored = Quantize(raw)
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(left, right keyed) int {
		if left.scored != right.scored {
			if left.scored {
				return -1
			}
			return 1
		}
		if left.scored && left.score != right.score {
			if left.score > right.score {
				return -1
			}
			return 1
		}
		return strings.Compare(left.id, right.id)
	})
	ordered := make([]string, len(rows))
	for i, row := range rows {
		ordered[i] = row.id
	}
	return ordered
}
