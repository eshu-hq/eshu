// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank_test

import (
	"encoding/base64"
	"math"
	"math/rand"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/search/rank"
)

func TestWalkRejectsGarbageAndTamperedCursors(t *testing.T) {
	t.Parallel()
	window := testWindow(40)
	first, err := rank.Walk(window, reverseScores(window), rank.StartToken, 10)
	if err != nil {
		t.Fatalf("first page error = %v", err)
	}
	valid := first.NextCursor
	if !strings.HasPrefix(valid, "rc1.") {
		t.Fatalf("cursor %q does not carry the rc1. version prefix", valid)
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(valid, "rc1."))
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	tampered := slices.Clone(payload)
	tampered[3] ^= 0x01 // served count byte
	cases := map[string]string{
		"empty":            "",
		"plain text":       "page-2",
		"wrong version":    "rc9." + strings.TrimPrefix(valid, "rc1."),
		"not base64":       "rc1.!!!!",
		"truncated":        valid[:len(valid)-4],
		"extra bytes":      "rc1." + base64.RawURLEncoding.EncodeToString(append(slices.Clone(payload), 0)),
		"tampered served":  "rc1." + base64.RawURLEncoding.EncodeToString(tampered),
		"padded base64":    valid + "==",
		"start with space": " start",
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := rank.Walk(window, reverseScores(window), token, 10)
			assertRankError(t, err, http.StatusBadRequest, rank.ErrorCodeInvalid, rank.ReasonMalformed)
		})
	}
}

func TestAdvanceMintsTheSameCursorTheWalkReturns(t *testing.T) {
	t.Parallel()
	window := testWindow(90)
	scores := reverseScores(window)
	first, err := rank.Walk(window, scores, rank.StartToken, 40)
	if err != nil {
		t.Fatalf("first page error = %v", err)
	}
	next, err := rank.Advance(first.PageCursor, first.IDs)
	if err != nil {
		t.Fatalf("Advance error = %v", err)
	}
	if next != first.NextCursor {
		t.Fatalf("Advance(page cursor, page ids) = %q, want the walk's next cursor %q", next, first.NextCursor)
	}
}

// TestAdvanceAfterAPrefixResumesWithoutLossOrRepeat models the MCP budget
// trim: a client is shown only the first k rows of a page, and the cursor
// minted for that prefix must resume at row k.
func TestAdvanceAfterAPrefixResumesWithoutLossOrRepeat(t *testing.T) {
	t.Parallel()
	window := testWindow(rank.WindowSize)
	scores := reverseScores(window)
	var served []string
	token := rank.StartToken
	for steps := 0; ; steps++ {
		if steps > 1000 {
			t.Fatal("walk did not end")
		}
		page, err := rank.Walk(window, scores, token, 60)
		if err != nil {
			t.Fatalf("Walk error = %v", err)
		}
		keep := len(page.IDs)
		if keep > 7 {
			keep = 7 + steps%5
		}
		served = append(served, page.IDs[:keep]...)
		if keep == len(page.IDs) && page.NextCursor == "" {
			break
		}
		token, err = rank.Advance(page.PageCursor, page.IDs[:keep])
		if err != nil {
			t.Fatalf("Advance error = %v", err)
		}
	}
	if want := rank.Order(windowIDs(window), scores); !slices.Equal(served, want) {
		t.Fatalf("trimmed walk differs from global order:\n got %v\nwant %v", served, want)
	}
}

func TestAdvanceRejectsMalformedCursors(t *testing.T) {
	t.Parallel()
	_, err := rank.Advance("garbage", []string{"a"})
	assertRankError(t, err, http.StatusBadRequest, rank.ErrorCodeInvalid, rank.ReasonMalformed)
	_, err = rank.Advance(rank.StartToken, []string{"a"})
	assertRankError(t, err, http.StatusBadRequest, rank.ErrorCodeInvalid, rank.ReasonMalformed)
}

func TestQuantizeKeepsEightSignificantDigits(t *testing.T) {
	t.Parallel()
	score := 1.0/61 + 1.0/63
	got, ok := rank.Quantize(score)
	if !ok {
		t.Fatalf("Quantize(%v) not ok", score)
	}
	for ulps := -4; ulps <= 4; ulps++ {
		nudged := nudgeULPs(score, ulps)
		if q, _ := rank.Quantize(nudged); q != got {
			t.Fatalf("Quantize(%v ulp %+d) = %v, want %v", score, ulps, q, got)
		}
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, ok := rank.Quantize(bad); ok {
			t.Fatalf("Quantize(%v) ok, want not ok", bad)
		}
	}
}

// TestWalkAcrossArchitecturesNeverSkipsOrRepeats is the cross-architecture
// tolerance contract. Each page is scored by a different "replica" whose
// scores differ from the reference by up to 4 ulp, the measured arm64-vs-amd64
// FMA difference (#7746), and some replicas also swap near-tied rows, as an
// approximate retriever on another architecture may. Every walk must either
// read each row exactly once or stop with a typed 409; it must never return a
// served row again or end without having served a row.
func TestWalkAcrossArchitecturesNeverSkipsOrRepeats(t *testing.T) {
	t.Parallel()
	conflicts, completed := 0, 0
	for seed := int64(1); seed <= 300; seed++ {
		rng := rand.New(rand.NewSource(seed))
		window := testWindow(50 + rng.Intn(151))
		reference := rrfLikeScores(window, rng)
		limit := 1 + rng.Intn(60)
		swapRate := []float64{0, 0, 0.05, 0.3}[seed%4]
		replica := func(int) map[string]float64 {
			scores := make(map[string]float64, len(reference))
			for id, score := range reference {
				scores[id] = nudgeULPs(score, rng.Intn(9)-4)
			}
			if rng.Float64() < swapRate {
				ids := windowIDs(window)
				a, b := ids[rng.Intn(len(ids))], ids[rng.Intn(len(ids))]
				scores[a], scores[b] = scores[b], scores[a]
			}
			return scores
		}
		served, err := walkAll(t, window, limit, replica)
		seen := make(map[string]bool, len(served))
		for _, id := range served {
			if seen[id] {
				t.Fatalf("seed %d: row %s served twice", seed, id)
			}
			seen[id] = true
		}
		if err != nil {
			assertRankError(t, err, http.StatusConflict, rank.ErrorCodeBasisMismatch, rank.ReasonRankOrderChanged)
			conflicts++
			continue
		}
		assertExactlyOnce(t, served, windowIDs(window))
		completed++
	}
	t.Logf("cross-architecture walks: %d/300 completed exactly once, %d/300 stopped with 409, 0 repeated or skipped a row", completed, conflicts)
	if completed == 0 || conflicts == 0 {
		t.Fatalf("completed %d walks and stopped %d with 409; want both outcomes exercised", completed, conflicts)
	}
}

// TestWalkUlpNoiseAloneDoesNotChangeTheOrder pins that FMA-sized noise on
// RRF-shaped scores does not move the walk: replicas that differ only by a few
// ulp produce the reference order and never a 409. The scores hold exact
// ties, as Reciprocal Rank Fusion does for two rows with swapped rank pairs
// (BM25 rank 3 and vector rank 7 against 7 and 3); ulp noise on raw bits
// would break each tie at random, and the quantized key keeps it a tie that
// the id breaks.
func TestWalkUlpNoiseAloneDoesNotChangeTheOrder(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(7746))
	window := testWindow(rank.WindowSize)
	reference := tiedRRFScores(window, rng)
	want := rank.Order(windowIDs(window), reference)
	got, err := walkAll(t, window, 50, func(int) map[string]float64 {
		scores := make(map[string]float64, len(reference))
		for id, score := range reference {
			scores[id] = nudgeULPs(score, rng.Intn(9)-4)
		}
		return scores
	})
	if err != nil {
		t.Fatalf("walk error = %v, want ulp noise to be absorbed", err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("ulp noise changed the walk order")
	}
}

// rrfLikeScores gives each row a Reciprocal Rank Fusion score from two
// random rank lists, the shape the hybrid ranker produces.
func rrfLikeScores(window rank.Window, rng *rand.Rand) map[string]float64 {
	n := len(window.Rows)
	bm25 := rng.Perm(n)
	vector := rng.Perm(n)
	scores := make(map[string]float64, n)
	for i, row := range window.Rows {
		scores[row.ID] = 1.0/float64(60+bm25[i]+1) + 1.0/float64(60+vector[i]+1)
	}
	return scores
}

// tiedRRFScores gives row i the rank pair (p[i], p[n-1-i]), so rows i and
// n-1-i carry exactly equal fused scores.
func tiedRRFScores(window rank.Window, rng *rand.Rand) map[string]float64 {
	n := len(window.Rows)
	perm := rng.Perm(n)
	scores := make(map[string]float64, n)
	for i, row := range window.Rows {
		scores[row.ID] = 1.0/float64(60+perm[i]+1) + 1.0/float64(60+perm[n-1-i]+1)
	}
	return scores
}

func nudgeULPs(score float64, ulps int) float64 {
	for ; ulps > 0; ulps-- {
		score = math.Nextafter(score, math.Inf(1))
	}
	for ; ulps < 0; ulps++ {
		score = math.Nextafter(score, math.Inf(-1))
	}
	return score
}
