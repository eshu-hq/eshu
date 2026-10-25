// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package epoch

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"testing"
)

const baseEpoch = "0123456789abcdef"

func bucket(name, input string, count int, output string) Bucket {
	return Bucket{Name: name, InputDigest: input, FactCount: count, OutputDigest: output}
}

// baseLedger is a sealed ledger with two file buckets and the catch-all.
func baseLedger() Ledger {
	buckets := []Bucket{
		bucket(RepositoryBucket, "in-repo", 3, "out-repo"),
		bucket("src/a.go", "in-a", 4, "out-a"),
		bucket("src/b.py", "in-b", 5, "out-b"),
	}
	return Ledger{Epoch: baseEpoch, Seal: SealOf(buckets), Buckets: buckets}
}

func replaced(buckets []Bucket, name string, next Bucket) []Bucket {
	out := make([]Bucket, 0, len(buckets))
	for _, b := range buckets {
		if b.Name == name {
			out = append(out, next)
			continue
		}
		out = append(out, b)
	}
	return out
}

func wantAdvance(parent string, buckets []Bucket) string {
	outputs := make([]string, 0, len(buckets))
	for _, b := range buckets {
		outputs = append(outputs, b.OutputDigest)
	}
	sortStrings(outputs)
	sum := sha256.Sum256([]byte(parent + "\n" + strings.Join(outputs, "\n")))
	return fmt.Sprintf("%x", sum)[:16]
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func TestParseRoundTrip(t *testing.T) {
	t.Parallel()
	ledger := baseLedger()
	parsed, err := Parse(ledger.Marshal())
	if err != nil {
		t.Fatalf("Parse(Marshal()) error = %v", err)
	}
	if string(parsed.Marshal()) != string(ledger.Marshal()) {
		t.Fatalf("round trip changed the ledger:\n%s\nwant\n%s", parsed.Marshal(), ledger.Marshal())
	}
	if !parsed.Sealed() {
		t.Fatalf("round-tripped ledger is not sealed")
	}
}

func TestParseRejectsMalformedLedgers(t *testing.T) {
	t.Parallel()
	good := string(baseLedger().Marshal())
	cases := map[string]string{
		"no epoch line":    strings.Replace(good, "\nepoch\t", "\n# epoch\t", 1),
		"two epoch lines":  good + "epoch\t" + baseEpoch + "\t" + baseLedger().Seal + "\n",
		"short bucket":     good + "src/c.rb\tin-c\t1\n",
		"bad fact count":   good + "src/c.rb\tin-c\tmany\tout-c\n",
		"duplicate bucket": good + "src/b.py\tin-b\t5\tout-b\n",
		"unsorted buckets": good + "src/0.rb\tin-0\t1\tout-0\n",
		"empty epoch":      strings.Replace(good, "epoch\t"+baseEpoch, "epoch\t", 1),
	}
	for name, body := range cases {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s: Parse accepted a malformed ledger", name)
		}
	}
}

// RED-A at the ledger seam: production output moved on unchanged input.
func TestRepinAdvancesWhenOutputMovesOnUnchangedInput(t *testing.T) {
	t.Parallel()
	pinned := baseLedger()
	computed := replaced(pinned.Buckets, "src/a.go", bucket("src/a.go", "in-a", 4, "out-a2"))
	next, err := Repin(pinned, computed, false)
	if err != nil {
		t.Fatalf("Repin error = %v", err)
	}
	if next.Epoch == pinned.Epoch {
		t.Fatalf("epoch did not advance on an output change with unchanged input")
	}
	if want := wantAdvance(pinned.Epoch, computed); next.Epoch != want {
		t.Fatalf("epoch = %s, want chained %s", next.Epoch, want)
	}
	if !ValidEpoch(next.Epoch) || !next.Sealed() {
		t.Fatalf("re-pinned ledger invalid: epoch %q sealed %t", next.Epoch, next.Sealed())
	}
}

// RED-B at the ledger seam: a fixture-only addition keeps the epoch.
func TestRepinKeepsEpochForFixtureOnlyChanges(t *testing.T) {
	t.Parallel()
	pinned := baseLedger()
	added := append(replaced(pinned.Buckets, RepositoryBucket, bucket(RepositoryBucket, "in-repo2", 3, "out-repo2")),
		bucket("src/c.rb", "in-c", 2, "out-c"))
	cases := map[string][]Bucket{
		"new bucket and whole-tree input moved": added,
		"input and output moved together":       replaced(pinned.Buckets, "src/b.py", bucket("src/b.py", "in-b2", 6, "out-b2")),
		"bucket removed":                        pinned.Buckets[:2],
	}
	for name, computed := range cases {
		next, err := Repin(pinned, computed, false)
		if err != nil {
			t.Fatalf("%s: Repin error = %v", name, err)
		}
		if next.Epoch != pinned.Epoch {
			t.Errorf("%s: epoch advanced to %s on a fixture-only change", name, next.Epoch)
		}
		if next.Seal == pinned.Seal || !next.Sealed() {
			t.Errorf("%s: seal not rewritten for the new bucket lines", name)
		}
	}
}

func TestRepinForceAdvances(t *testing.T) {
	t.Parallel()
	pinned := baseLedger()
	next, err := Repin(pinned, pinned.Buckets, true)
	if err != nil {
		t.Fatalf("Repin error = %v", err)
	}
	if next.Epoch == pinned.Epoch || next.Epoch != wantAdvance(pinned.Epoch, pinned.Buckets) {
		t.Fatalf("forced re-pin epoch = %s, want chained advance from %s", next.Epoch, pinned.Epoch)
	}
	same, err := Repin(pinned, pinned.Buckets, false)
	if err != nil || string(same.Marshal()) != string(pinned.Marshal()) {
		t.Fatalf("unforced re-pin of an unchanged tree changed the ledger (err %v)", err)
	}
}

// RED-C at the ledger seam: two branches from one base that move different
// buckets land on different epochs, and the combined tree re-pinned from
// main's ledger lands on a third. A hand merge that keeps both bucket lines
// and one side's epoch line is unsealed and refused.
func TestRepinChainsConcurrentBranches(t *testing.T) {
	t.Parallel()
	base := baseLedger()
	aBuckets := replaced(base.Buckets, "src/a.go", bucket("src/a.go", "in-a", 4, "out-a2"))
	bBuckets := replaced(base.Buckets, "src/b.py", bucket("src/b.py", "in-b", 5, "out-b2"))
	a, errA := Repin(base, aBuckets, false)
	b, errB := Repin(base, bBuckets, false)
	if errA != nil || errB != nil {
		t.Fatalf("branch re-pin errors: %v, %v", errA, errB)
	}
	if a.Epoch == b.Epoch || a.Epoch == base.Epoch || b.Epoch == base.Epoch {
		t.Fatalf("branch epochs not distinct: base %s a %s b %s", base.Epoch, a.Epoch, b.Epoch)
	}

	combined := replaced(aBuckets, "src/b.py", bucket("src/b.py", "in-b", 5, "out-b2"))
	handMerged := Ledger{Epoch: a.Epoch, Seal: a.Seal, Buckets: combined}
	if handMerged.Sealed() {
		t.Fatalf("hand-merged ledger reads as sealed")
	}
	if _, err := Repin(handMerged, combined, false); err == nil {
		t.Fatalf("Repin accepted a hand-merged ledger whose seal does not match its bucket lines")
	}

	c, err := Repin(a, combined, false)
	if err != nil {
		t.Fatalf("combined re-pin error = %v", err)
	}
	for name, other := range map[string]string{"base": base.Epoch, "a": a.Epoch, "b": b.Epoch} {
		if c.Epoch == other {
			t.Fatalf("combined epoch %s equals %s epoch", c.Epoch, name)
		}
	}
}

// Two branches that make the same output move emit the same facts, so they
// must share an epoch; their ledgers are byte-identical and merge cleanly.
func TestRepinSharesEpochForIdenticalOutputMoves(t *testing.T) {
	t.Parallel()
	base := baseLedger()
	moved := replaced(base.Buckets, "src/a.go", bucket("src/a.go", "in-a", 4, "out-a2"))
	a, errA := Repin(base, moved, false)
	b, errB := Repin(base, append([]Bucket(nil), moved...), false)
	if errA != nil || errB != nil {
		t.Fatalf("re-pin errors: %v, %v", errA, errB)
	}
	if a.Epoch == base.Epoch {
		t.Fatalf("output move kept epoch %s", base.Epoch)
	}
	if string(a.Marshal()) != string(b.Marshal()) {
		t.Fatalf("identical output moves wrote different ledgers:\n%s\n%s", a.Marshal(), b.Marshal())
	}
}

func TestDriftNamesEveryDifference(t *testing.T) {
	t.Parallel()
	pinned := baseLedger()
	if got := Drift(pinned, pinned.Buckets); len(got) != 0 {
		t.Fatalf("Drift on an identical tree = %v, want none", got)
	}
	computed := append(replaced(pinned.Buckets[1:], "src/a.go", bucket("src/a.go", "in-a", 4, "out-a2")),
		bucket("src/c.rb", "in-c", 2, "out-c"))
	got := strings.Join(Drift(pinned, computed), "\n")
	for _, want := range []string{"src/a.go", "src/c.rb", RepositoryBucket} {
		if !strings.Contains(got, want) {
			t.Errorf("Drift output does not name %s:\n%s", want, got)
		}
	}
	unsealed := pinned
	unsealed.Seal = strings.Repeat("0", 64)
	if got := strings.Join(Drift(unsealed, pinned.Buckets), "\n"); !strings.Contains(got, "origin/main") {
		t.Errorf("Drift on an unsealed ledger does not give the restore recipe:\n%s", got)
	}
}

// TestCurrentIsTheCommittedEpoch pins the production read: the embedded
// ledger is the committed file, and its epoch is a chained 16-hex value.
func TestCurrentIsTheCommittedEpoch(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(LedgerFile)
	if err != nil {
		t.Fatalf("read %s: %v", LedgerFile, err)
	}
	committed, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", LedgerFile, err)
	}
	if got := Current(); got != committed.Epoch || !ValidEpoch(got) {
		t.Fatalf("Current() = %q, committed epoch %q (valid %t)", got, committed.Epoch, ValidEpoch(got))
	}
	if !committed.Sealed() {
		t.Fatalf("%s seal does not match its bucket lines: restore origin/main's copy and re-pin", LedgerFile)
	}
}

func TestValidEpoch(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]bool{
		baseEpoch: true, "git-collector.v14": false, "0123456789ABCDEF": false, "0123": false, "": false,
	} {
		if got := ValidEpoch(value); got != want {
			t.Errorf("ValidEpoch(%q) = %t, want %t", value, got, want)
		}
	}
}

// TestMarshalSeparatesEpochFromBuckets keeps a fixed line between the epoch
// line and the first bucket line, so git never merges a change to the first
// bucket into the epoch line's conflict hunk.
func TestMarshalSeparatesEpochFromBuckets(t *testing.T) {
	t.Parallel()
	lines := strings.Split(string(baseLedger().Marshal()), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "epoch\t") {
			continue
		}
		if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "#") {
			t.Fatalf("line after the epoch line is %q, want a fixed comment line", lines[i+1])
		}
		return
	}
	t.Fatalf("no epoch line in the marshaled ledger")
}

// TestHandCombinedLedgerIsRefusedUntilParentRestored is RED-D from the #9097
// ruling (addendum 1). The file a careless conflict resolution leaves behind,
// the union of two branches' bucket lines under either side's epoch line,
// would re-pin with no advance and ship new output under an old hint. The
// drift check fails on it with the restore recipe and Repin refuses it;
// restoring the parent (main's) copy and re-pinning advances the epoch to a
// value no branch or base used, with a consistent seal.
func TestHandCombinedLedgerIsRefusedUntilParentRestored(t *testing.T) {
	t.Parallel()
	const recipe = "ledger was merged by hand: git checkout origin/main -- go/internal/collector/repo/git/epoch/ledger.tsv, then re-pin"
	base := baseLedger()
	aBuckets := replaced(base.Buckets, "src/a.go", bucket("src/a.go", "in-a", 4, "out-a2"))
	bBuckets := replaced(base.Buckets, "src/b.py", bucket("src/b.py", "in-b", 5, "out-b2"))
	a, errA := Repin(base, aBuckets, false)
	b, errB := Repin(base, bBuckets, false)
	if errA != nil || errB != nil {
		t.Fatalf("branch re-pin errors: %v, %v", errA, errB)
	}
	combined := replaced(aBuckets, "src/b.py", bucket("src/b.py", "in-b", 5, "out-b2"))

	for name, side := range map[string]Ledger{"main's epoch line": a, "branch's epoch line": b} {
		hand, err := Parse(Ledger{Epoch: side.Epoch, Seal: side.Seal, Buckets: combined}.Marshal())
		if err != nil {
			t.Fatalf("%s: hand-combined ledger does not parse: %v", name, err)
		}
		drift := Drift(hand, combined)
		if len(drift) != 1 || !strings.Contains(drift[0], recipe) {
			t.Errorf("%s: Drift = %q, want one line with %q", name, drift, recipe)
		}
		if _, err := Repin(hand, combined, false); err == nil || !strings.Contains(err.Error(), recipe) {
			t.Errorf("%s: Repin error = %v, want a refusal with %q", name, err, recipe)
		}
	}

	parent, err := Parse(a.Marshal())
	if err != nil {
		t.Fatalf("parse restored parent: %v", err)
	}
	c, err := Repin(parent, combined, false)
	if err != nil {
		t.Fatalf("re-pin from the restored parent: %v", err)
	}
	for name, other := range map[string]string{"base": base.Epoch, "main": a.Epoch, "branch": b.Epoch} {
		if c.Epoch == other {
			t.Errorf("re-pinned epoch %s equals the %s epoch", c.Epoch, name)
		}
	}
	if !ValidEpoch(c.Epoch) || !c.Sealed() || len(Drift(c, combined)) != 0 {
		t.Fatalf("re-pinned ledger not consistent: epoch %q sealed %t drift %v", c.Epoch, c.Sealed(), Drift(c, combined))
	}
}
