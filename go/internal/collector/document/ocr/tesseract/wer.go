// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"strings"
	"unicode"
)

// werScore is the word-level alignment behind the #10073 accuracy eval:
// WER = (S + D + I) / N where S/D/I come from word-level Levenshtein
// alignment of the hypothesis against the reference and N is the reference
// word count. Normalization before alignment lower-cases, strips
// punctuation, and collapses whitespace runs, matching the eval plan and the
// spoken-fold convention in the media engine.
type werScore struct {
	// RefWords is N, the reference word count after normalization.
	RefWords int
	// Sub, Del, Ins are the substitution, deletion, and insertion counts.
	Sub int
	Del int
	Ins int
	// WER is (S + D + I) / N, or 0 when both sides normalize empty and 1
	// when the reference is empty but the hypothesis is not.
	WER float64
	// WERRaw repeats the alignment without case/punctuation folding, for
	// audit against the folded WER.
	WERRaw float64
	// CER is the character-level error rate on the folded strings, the
	// secondary OCR metric: heavy noise degrades characters more
	// gracefully than whole words.
	CER float64
}

// scoreWER aligns hypothesis against reference at the word level.
func scoreWER(reference, hypothesis string) werScore {
	ref := normalizeWERWords(reference)
	hyp := normalizeWERWords(hypothesis)
	sub, del, ins := alignWERCounts(ref, hyp)
	out := werScore{RefWords: len(ref), Sub: sub, Del: del, Ins: ins}
	out.WER = errorRate(sub, del, ins, len(ref), len(hyp))
	rawRef := strings.Fields(reference)
	rawHyp := strings.Fields(hypothesis)
	rawSub, rawDel, rawIns := alignWERCounts(rawRef, rawHyp)
	out.WERRaw = errorRate(rawSub, rawDel, rawIns, len(rawRef), len(rawHyp))
	out.CER = characterErrorRate(strings.Join(ref, " "), strings.Join(hyp, " "))
	return out
}

// normalizeWERWords lower-cases, maps every non-letter non-digit rune to a
// word break, and collapses whitespace runs via strings.Fields.
func normalizeWERWords(s string) []string {
	folded := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, s)
	return strings.Fields(folded)
}

// alignWERCounts returns the (substitution, deletion, insertion) counts from
// the word-level Levenshtein alignment of hyp against ref.
func alignWERCounts(ref, hyp []string) (sub, del, ins int) {
	// Full table, then a traceback that splits the distance into S/D/I.
	dist := make([][]int, len(ref)+1)
	for r := range dist {
		dist[r] = make([]int, len(hyp)+1)
	}
	for r := range dist {
		dist[r][0] = r
	}
	for c := range dist[0] {
		dist[0][c] = c
	}
	for r := 1; r <= len(ref); r++ {
		for c := 1; c <= len(hyp); c++ {
			cost := 1
			if ref[r-1] == hyp[c-1] {
				cost = 0
			}
			dist[r][c] = min3(dist[r-1][c]+1, dist[r][c-1]+1, dist[r-1][c-1]+cost)
		}
	}
	i, j := len(ref), len(hyp)
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && ref[i-1] == hyp[j-1] && dist[i][j] == dist[i-1][j-1]:
			i--
			j--
		case i > 0 && j > 0 && dist[i][j] == dist[i-1][j-1]+1:
			sub++
			i--
			j--
		case i > 0 && dist[i][j] == dist[i-1][j]+1:
			del++
			i--
		case j > 0 && dist[i][j] == dist[i][j-1]+1:
			ins++
			j--
		default:
			// Ties resolve toward substitution, then deletion, then
			// insertion, so every step makes progress.
			if i > 0 && j > 0 {
				sub++
				i--
				j--
			} else if i > 0 {
				del++
				i--
			} else {
				ins++
				j--
			}
		}
	}
	return sub, del, ins
}

// characterErrorRate returns the rune-level Levenshtein distance over N
// reference runes, the secondary OCR metric for heavy noise.
func characterErrorRate(reference, hypothesis string) float64 {
	ref := []rune(reference)
	hyp := []rune(hypothesis)
	prev := make([]int, len(hyp)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ref); i++ {
		cur := make([]int, len(hyp)+1)
		cur[0] = i
		for j := 1; j <= len(hyp); j++ {
			cost := 1
			if ref[i-1] == hyp[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return errorRate(0, 0, prev[len(hyp)], len(ref), len(hyp))
}

// errorRate divides edits by the reference length, with the empty-reference
// convention: 0 when the hypothesis is also empty, 1 otherwise.
func errorRate(sub, del, ins, refLen, hypLen int) float64 {
	if refLen == 0 {
		if hypLen == 0 {
			return 0
		}
		return 1
	}
	return float64(sub+del+ins) / float64(refLen)
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
