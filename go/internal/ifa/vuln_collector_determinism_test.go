// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa/familyodu"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// shuffleCount is how many seeded permutations each subject runs under. The
// seeds are fixed, so a failure names a permutation that can be replayed.
const shuffleCount = 32

// determinismSubject is one fact set the impact builder is run over in several
// delivery orders. wantFindings guards against vacuity: an equal digest of two
// empty results proves nothing, so each subject names how many findings its
// baseline must hold.
type determinismSubject struct {
	name         string
	odu          familyodu.Odu
	wantFindings int
}

// withZonedStamps returns clones of the facts whose vulnerability.cve payloads
// carry RFC 3339 zones on their timestamps.
func withZonedStamps(input []facts.Envelope) []facts.Envelope {
	out := cloneFacts(input)
	for _, fact := range out {
		if fact.FactKind != "vulnerability.cve" {
			continue
		}
		for _, key := range []string{"modified_at", "published_at"} {
			if stamp, ok := fact.Payload[key].(string); ok && stamp != "" && !strings.HasSuffix(stamp, "Z") {
				fact.Payload[key] = stamp + "Z"
			}
		}
	}
	return out
}

// determinismSubjects returns the Odùs worth permuting: every one that reaches
// a finding, plus the cross-window combination, where the newest-copy logic
// compares three windows' copies of the same CVEs. Each stamp format appears,
// because the reducer reads the recorded zone-less stamp and an RFC 3339 stamp
// through different parses before it ranks the copies.
func determinismSubjects(t *testing.T) []determinismSubject {
	t.Helper()

	allScopes := []string{vulnWindow1Scope, vulnWindow2Scope, vulnWindowXScope, vulnKEVScope, vulnEPSSScope}
	allCPEs := []string{vulnWidgetCPE, vulnLegacyToolCPE, vulnVectorOnlyCPE, vulnLabelOnlyCPE, vulnBaselineOnlyCPE}
	everything := adHocOdu(t, allScopes, allCPEs...)
	zonedEverything := everything
	zonedEverything.Facts = withZonedStamps(everything.Facts)

	newestCopy := catalogOdu(t, VulnCollectorNewestCopyOduName)
	zonedNewestCopy := newestCopy
	zonedNewestCopy.Facts = withZonedStamps(newestCopy.Facts)

	return []determinismSubject{
		{"product path", catalogOdu(t, VulnCollectorOduName), 1},
		{"rejected", catalogOdu(t, VulnCollectorRejectedOduName), 1},
		{"newest copy, zone-less as recorded", newestCopy, 1},
		{"newest copy, RFC 3339 stamps", zonedNewestCopy, 1},
		{"severity tiers", adHocOdu(t, []string{vulnWindow1Scope, vulnKEVScope, vulnEPSSScope},
			vulnWidgetCPE, vulnVectorOnlyCPE, vulnLabelOnlyCPE), 3},
		{"older window selected", adHocOdu(t, []string{vulnWindowXScope}, vulnBaselineOnlyCPE), 1},
		{"every window, zone-less as recorded", everything, 5},
		{"every window, RFC 3339 stamps", zonedEverything, 5},
	}
}

// findingsDigest is the sha256 of the findings' JSON. The builder sorts its
// findings, so the digest covers their order as well as their content.
func findingsDigest(t *testing.T, findings []core.SupplyChainImpactFinding) string {
	t.Helper()
	raw, err := json.Marshal(findings)
	if err != nil {
		t.Fatalf("marshal findings: %v", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// digestInOrder runs the builder over a deep copy of input in the given order.
func digestInOrder(t *testing.T, order []facts.Envelope) string {
	t.Helper()
	return findingsDigest(t, core.BuildSupplyChainImpactFindings(cloneFacts(order)))
}

// baselineDigest returns the digest of the facts in their recorded order and
// fails when the subject does not hold the findings it promises.
func baselineDigest(t *testing.T, subject determinismSubject) string {
	t.Helper()
	findings := core.BuildSupplyChainImpactFindings(cloneFacts(subject.odu.Facts))
	if len(findings) != subject.wantFindings {
		t.Fatalf("%s: baseline findings = %d, want %d; an equal digest of the wrong set would prove nothing",
			subject.name, len(findings), subject.wantFindings)
	}
	return findingsDigest(t, findings)
}

// TestFindingsDigestCoversOrderAndContent checks the instrument the tests below
// rest on. A digest that ignored the order of findings, or one of their fields,
// would let any delivery order pass.
func TestFindingsDigestCoversOrderAndContent(t *testing.T) {
	t.Parallel()

	odu := adHocOdu(t, []string{vulnWindow1Scope, vulnKEVScope, vulnEPSSScope},
		vulnWidgetCPE, vulnVectorOnlyCPE, vulnLabelOnlyCPE)
	findings := core.BuildSupplyChainImpactFindings(cloneFacts(odu.Facts))
	if len(findings) != 3 {
		t.Fatalf("findings = %d, want 3", len(findings))
	}
	want := findingsDigest(t, findings)

	if again := findingsDigest(t, core.BuildSupplyChainImpactFindings(cloneFacts(odu.Facts))); again != want {
		t.Errorf("two runs over the same facts differ: %s and %s", want, again)
	}

	reordered := slices.Clone(findings)
	slices.Reverse(reordered)
	if findingsDigest(t, reordered) == want {
		t.Error("reversing the findings left the digest unchanged: it does not cover their order")
	}

	edited := slices.Clone(findings)
	edited[1].CVSSScore += 0.1
	if findingsDigest(t, edited) == want {
		t.Error("editing a finding's score left the digest unchanged: it does not cover their content")
	}
}

func TestVulnCollectorFindingsAreOrderIndependent(t *testing.T) {
	t.Parallel()

	for _, subject := range determinismSubjects(t) {
		t.Run(subject.name, func(t *testing.T) {
			t.Parallel()
			want := baselineDigest(t, subject)

			reversed := slices.Clone(subject.odu.Facts)
			slices.Reverse(reversed)
			if got := digestInOrder(t, reversed); got != want {
				t.Errorf("reversed delivery changed the findings: digest %s, want %s", got, want)
			}

			for seed := uint64(1); seed <= shuffleCount; seed++ {
				order := slices.Clone(subject.odu.Facts)
				rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
				rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
				if got := digestInOrder(t, order); got != want {
					t.Fatalf("shuffle seed %d changed the findings: digest %s, want %s", seed, got, want)
				}
			}
		})
	}
}

// contiguousChunks splits input into n runs of near-equal length.
func contiguousChunks(input []facts.Envelope, n int) [][]facts.Envelope {
	chunks := make([][]facts.Envelope, n)
	size := (len(input) + n - 1) / n
	for i := range chunks {
		lo := min(i*size, len(input))
		hi := min(lo+size, len(input))
		chunks[i] = input[lo:hi]
	}
	return chunks
}

// stridedChunks deals input out round-robin, so each chunk holds every n-th
// fact: the delivery a hash-partitioned load produces.
func stridedChunks(input []facts.Envelope, n int) [][]facts.Envelope {
	chunks := make([][]facts.Envelope, n)
	for i, fact := range input {
		chunks[i%n] = append(chunks[i%n], fact)
	}
	return chunks
}

// scopeChunks groups input by scope and generation, in sorted key order: the
// delivery of a load that pages one partition at a time.
func scopeChunks(input []facts.Envelope) [][]facts.Envelope {
	byKey := map[string][]facts.Envelope{}
	for _, fact := range input {
		key := fact.ScopeGenerationKey()
		byKey[key] = append(byKey[key], fact)
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	chunks := make([][]facts.Envelope, 0, len(keys))
	for _, key := range keys {
		chunks = append(chunks, byKey[key])
	}
	return chunks
}

// concatChunks joins chunks in the given order, reversed when reverse is set.
func concatChunks(chunks [][]facts.Envelope, reverse bool) []facts.Envelope {
	ordered := slices.Clone(chunks)
	if reverse {
		slices.Reverse(ordered)
	}
	var out []facts.Envelope
	for _, chunk := range ordered {
		out = append(out, chunk...)
	}
	return out
}

// TestVulnCollectorFindingsArePartitionIndependent delivers each subject's
// facts as N chunks, for N of 1, 2 and 4, and as one chunk per scope. The chunk
// shapes are the ones a partitioned load produces, so they reach orders a random
// shuffle seldom does: the newer window ahead of the older, and KEV ahead of
// the CVEs it annotates.
func TestVulnCollectorFindingsArePartitionIndependent(t *testing.T) {
	t.Parallel()

	for _, subject := range determinismSubjects(t) {
		t.Run(subject.name, func(t *testing.T) {
			t.Parallel()
			want := baselineDigest(t, subject)

			type delivery struct {
				label string
				order []facts.Envelope
			}
			var deliveries []delivery
			for _, n := range []int{1, 2, 4} {
				contiguous := contiguousChunks(subject.odu.Facts, n)
				strided := stridedChunks(subject.odu.Facts, n)
				deliveries = append(deliveries,
					delivery{fmt.Sprintf("N=%d contiguous, reversed chunk order", n), concatChunks(contiguous, true)},
					delivery{fmt.Sprintf("N=%d strided", n), concatChunks(strided, false)},
					delivery{fmt.Sprintf("N=%d strided, reversed chunk order", n), concatChunks(strided, true)},
				)
			}
			byScope := scopeChunks(subject.odu.Facts)
			deliveries = append(deliveries,
				delivery{fmt.Sprintf("one chunk per scope (%d), in scope order", len(byScope)), concatChunks(byScope, false)},
				delivery{fmt.Sprintf("one chunk per scope (%d), reversed", len(byScope)), concatChunks(byScope, true)},
			)

			for _, d := range deliveries {
				if len(d.order) != len(subject.odu.Facts) {
					t.Fatalf("%s: delivered %d facts, want %d: the chunking lost or repeated facts", d.label, len(d.order), len(subject.odu.Facts))
				}
				if got := digestInOrder(t, d.order); got != want {
					t.Errorf("%s changed the findings: digest %s, want %s", d.label, got, want)
				}
			}
		})
	}
}
