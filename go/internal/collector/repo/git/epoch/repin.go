// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package epoch

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

// handMergeRefusal is the text for a ledger whose seal does not match its
// bucket lines, with the remedy: only main's copy is a ledger a deployed binary
// ran under (#9097 ruling, addendum 1).
const handMergeRefusal = "ledger was merged by hand: git checkout origin/main -- go/internal/collector/repo/git/epoch/ledger.tsv, then re-pin with -update-content-version-pin"

// Repin returns the ledger for the computed buckets, chained from pinned.
//
// The epoch advances when force is set or when some bucket keeps its input
// digest and changes its output digest: the collector now emits different
// facts for unchanged source, so every deployed hint must move. A new bucket,
// a removed bucket, or a bucket whose input changed is a fixture change and
// keeps the epoch, so it stores nothing new in deployed repositories. The
// advanced epoch is the first 16 hex characters of
// sha256(parent epoch + "\n" + the sorted output digests joined by "\n"), so
// two branches that advance from one parent with different outputs land on
// different epochs, and a re-pin of their combined tree from main's ledger
// lands on a third. Branches that move output identically share an epoch,
// because their facts are the same.
//
// Repin refuses a pinned ledger that is not Sealed: its bucket lines were
// combined by hand, so it cannot tell which outputs a deployed binary saw.
func Repin(pinned Ledger, computed []Bucket, force bool) (Ledger, error) {
	if !pinned.Sealed() {
		return Ledger{}, fmt.Errorf("%s (its seal does not match its bucket lines)", handMergeRefusal)
	}
	buckets := sortedBuckets(computed)
	next := Ledger{Epoch: pinned.Epoch, Seal: SealOf(buckets), Buckets: buckets}
	if force || outputMovedOnUnchangedInput(pinned.Buckets, buckets) {
		next.Epoch = advance(pinned.Epoch, buckets)
	}
	return next, nil
}

// Drift lists every way the pinned ledger differs from the ledger Repin would
// write for computed, one line per difference. An empty result means the
// committed ledger is current.
func Drift(pinned Ledger, computed []Bucket) []string {
	if !pinned.Sealed() {
		return []string{handMergeRefusal + " (its seal does not match its bucket lines)"}
	}
	buckets := sortedBuckets(computed)
	old := map[string]Bucket{}
	for _, b := range pinned.Buckets {
		old[b.Name] = b
	}
	var drift []string
	for _, b := range buckets {
		was, ok := old[b.Name]
		delete(old, b.Name)
		switch {
		case !ok:
			drift = append(drift, fmt.Sprintf("bucket %s is not pinned (%d facts)", b.Name, b.FactCount))
		case was.InputDigest == b.InputDigest && was.OutputDigest != b.OutputDigest:
			drift = append(drift, fmt.Sprintf("bucket %s: output moved on unchanged input (%d facts, %s; pinned %d facts, %s): the re-pin advances the epoch",
				b.Name, b.FactCount, b.OutputDigest, was.FactCount, was.OutputDigest))
		case was != b:
			drift = append(drift, fmt.Sprintf("bucket %s: input or output moved (pinned %+v, now %+v)", b.Name, was, b))
		}
	}
	orphans := make([]string, 0, len(old))
	for name := range old {
		orphans = append(orphans, name)
	}
	sort.Strings(orphans)
	for _, name := range orphans {
		drift = append(drift, fmt.Sprintf("bucket %s is pinned but no longer emitted", name))
	}
	return drift
}

func outputMovedOnUnchangedInput(pinned, computed []Bucket) bool {
	old := make(map[string]Bucket, len(pinned))
	for _, b := range pinned {
		old[b.Name] = b
	}
	for _, b := range computed {
		if was, ok := old[b.Name]; ok && was.InputDigest == b.InputDigest && was.OutputDigest != b.OutputDigest {
			return true
		}
	}
	return false
}

func advance(parent string, buckets []Bucket) string {
	outputs := make([]string, 0, len(buckets))
	for _, b := range buckets {
		outputs = append(outputs, b.OutputDigest)
	}
	sort.Strings(outputs)
	sum := sha256.Sum256([]byte(parent + "\n" + strings.Join(outputs, "\n")))
	return fmt.Sprintf("%x", sum)[:epochLength]
}

func sortedBuckets(buckets []Bucket) []Bucket {
	out := append([]Bucket(nil), buckets...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
