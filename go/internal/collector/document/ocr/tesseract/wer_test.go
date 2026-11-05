// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"math"
	"testing"
)

// TestScoreWERContract pins the word-level Levenshtein WER definition from
// the #10073 eval plan: WER = (S + D + I) / N over lower-cased,
// punctuation-stripped, whitespace-collapsed word tokens.
func TestScoreWERContract(t *testing.T) {
	cases := []struct {
		name      string
		reference string
		hyp       string
		sub       int
		del       int
		ins       int
		refWords  int
		wer       float64
	}{
		{name: "identical", reference: "Deploy the Checkout Service to staging", hyp: "Deploy the Checkout Service to staging", refWords: 6, wer: 0},
		{name: "empty both", reference: "", hyp: "", refWords: 0, wer: 0},
		{name: "empty hypothesis", reference: "Deploy the Checkout", hyp: "", del: 3, refWords: 3, wer: 1},
		{name: "empty reference", reference: "", hyp: "hello", ins: 1, refWords: 0, wer: 1},
		{name: "one substitution", reference: "Deploy the Checkout", hyp: "Deploy a Checkout", sub: 1, refWords: 3, wer: 1.0 / 3},
		{name: "one deletion", reference: "Deploy the Checkout Service", hyp: "Deploy Checkout Service", del: 1, refWords: 4, wer: 0.25},
		{name: "one insertion", reference: "Deploy Checkout", hyp: "Deploy the Checkout", ins: 1, refWords: 2, wer: 0.5},
		{name: "case fold", reference: "Deploy The Checkout", hyp: "deploy the checkout", refWords: 3, wer: 0},
		{name: "punctuation fold", reference: "Password: Hunter2", hyp: "password hunter2", refWords: 2, wer: 0},
		{name: "whitespace fold", reference: "Deploy   the\nCheckout", hyp: "Deploy the Checkout", refWords: 3, wer: 0},
		{name: "misheard password line", reference: "The password is as follows", hyp: "The password in a column", sub: 3, refWords: 5, wer: 0.6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scoreWER(tc.reference, tc.hyp)
			if got.RefWords != tc.refWords || got.Sub != tc.sub || got.Del != tc.del || got.Ins != tc.ins {
				t.Fatalf("scoreWER(%q, %q) = %+v, want ref=%d S=%d D=%d I=%d",
					tc.reference, tc.hyp, got, tc.refWords, tc.sub, tc.del, tc.ins)
			}
			if math.Abs(got.WER-tc.wer) > 1e-9 {
				t.Fatalf("scoreWER(%q, %q) WER = %v, want %v", tc.reference, tc.hyp, got.WER, tc.wer)
			}
		})
	}
}

// TestScoreWERCharacterBackoff pins CER as the secondary OCR metric: heavy
// noise degrades characters more gracefully than whole words.
func TestScoreWERCharacterBackoff(t *testing.T) {
	if got := characterErrorRate("staging", "staging"); got != 0 {
		t.Fatalf("identical CER = %v, want 0", got)
	}
	if got := characterErrorRate("staging", "stagin"); math.Abs(got-1.0/7) > 1e-9 {
		t.Fatalf("one deletion CER = %v, want %v", got, 1.0/7)
	}
	if got := characterErrorRate("", ""); got != 0 {
		t.Fatalf("empty CER = %v, want 0", got)
	}
}
