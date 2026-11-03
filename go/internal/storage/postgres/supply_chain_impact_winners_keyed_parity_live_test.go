// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"math/rand"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestSupplyChainImpactWinnersResweepKeyedMatchesRecomputeAcrossChangeClassesLive is
// the #9805 accuracy proof. Every change class goes through its production
// capture path, one keyed tick runs, and the winners table must equal a fresh
// full recompute: same keys, same 23 data columns. A class the capture misses
// fails at its own step.
func TestSupplyChainImpactWinnersResweepKeyedMatchesRecomputeAcrossChangeClassesLive(t *testing.T) {
	h := newKeyedHarness(t, "#9805 keyed parity proof")
	const (
		scopeA = "vuln-intel://9805/a"
		scopeB = "vuln-intel://9805/b"
		scopeC = "vuln-intel://9805/c"
	)
	h.seedScope(scopeA, scopeA+"/g1")
	h.seedScope(scopeB, scopeB+"/g1")
	h.seedScope(scopeC, scopeC+"/g1")
	// An operator generation with a free-form payload that has no package_id
	// and no subject_digest, like the suppression store writes.
	h.seedOperator("op-g1", "", map[string]map[string]any{
		"op-noise": {"cve_id": "CVE-2026-0999", "suppression_state": "accepted_risk", "repository_id": "repo-9805"},
	})

	// Seed the table from a full resweep (the startup reconcile), then drain the
	// capture the seeding wrote.
	h.write(scopeA, scopeA+"/g1",
		keyedFinding("CVE-2026-0001", "pkg:npm/a", 5), keyedFinding("CVE-2026-0002", "pkg:npm/a", 5),
		keyedFinding("CVE-2026-0003", "pkg:npm/b", 4), keyedAdvisoryFinding("GHSA-9805-aaaa", "pkg:npm/adv", 3))
	h.write(scopeB, scopeB+"/g1", keyedFinding("CVE-2026-0001", "pkg:npm/a", 9))
	hot := make([]supplychaincore.SupplyChainImpactFinding, 0, 18)
	for i := 0; i < 18; i++ {
		hot = append(hot, keyedFinding(fmt.Sprintf("CVE-2026-1%03d", i), "pkg:npm/hot", 2))
	}
	h.write(scopeC, scopeC+"/g1", hot...)
	h.keyed()
	h.assertParity("first keyed tick over an empty table")

	t.Run("same-generation rewrite flips a winner with ties", func(t *testing.T) {
		h.write(scopeA, scopeA+"/g1",
			keyedFinding("CVE-2026-0001", "pkg:npm/a", 20), keyedFinding("CVE-2026-0002", "pkg:npm/a", 5),
			keyedFinding("CVE-2026-0003", "pkg:npm/b", 4), keyedAdvisoryFinding("GHSA-9805-aaaa", "pkg:npm/adv", 3))
		if got := h.keyed(); got.Updated == 0 {
			t.Fatalf("keyed tick = %+v, want the flipped winner updated", got)
		}
		h.assertParity("priority rewrite")
	})
	t.Run("in-place tombstone flip through the retract path", func(t *testing.T) {
		h.write(scopeA, scopeA+"/g1",
			keyedFinding("CVE-2026-0001", "pkg:npm/a", 20), keyedFinding("CVE-2026-0002", "pkg:npm/a", 5),
			keyedAdvisoryFinding("GHSA-9805-aaaa", "pkg:npm/adv", 3))
		if got := h.keyed(); got.Deleted == 0 {
			t.Fatalf("keyed tick = %+v, want the retracted key deleted", got)
		}
		h.assertParity("retract")
	})
	t.Run("new candidate fact for an existing key", func(t *testing.T) {
		h.write(scopeC, scopeC+"/g1", append(append([]supplychaincore.SupplyChainImpactFinding(nil), hot...),
			keyedFinding("CVE-2026-0002", "pkg:npm/a", 6))...)
		h.keyed()
		h.assertParity("new candidate")
	})
	t.Run("hot package: one rewrite among eighteen keys of one triple", func(t *testing.T) {
		hot[3] = keyedFinding("CVE-2026-1003", "pkg:npm/hot", 15)
		h.write(scopeC, scopeC+"/g1", append(append([]supplychaincore.SupplyChainImpactFinding(nil), hot...),
			keyedFinding("CVE-2026-0002", "pkg:npm/a", 6))...)
		h.keyed()
		h.assertParity("hot package")
	})
	t.Run("several dirty keys on one hot triple and one on another triple in a single tick", func(t *testing.T) {
		hot[5] = keyedFinding("CVE-2026-1005", "pkg:npm/hot", 40)
		hot[11] = keyedFinding("CVE-2026-1011", "pkg:npm/hot", 41)
		hot[17] = keyedFinding("CVE-2026-1017", "pkg:npm/hot", 42)
		h.write(scopeC, scopeC+"/g1", append(append([]supplychaincore.SupplyChainImpactFinding(nil), hot...),
			keyedFinding("CVE-2026-0002", "pkg:npm/a", 6))...)
		h.write(scopeA, scopeA+"/g1",
			keyedFinding("CVE-2026-0001", "pkg:npm/a", 21), keyedFinding("CVE-2026-0002", "pkg:npm/a", 5),
			keyedAdvisoryFinding("GHSA-9805-aaaa", "pkg:npm/adv", 3))
		got := h.keyed()
		if got.Path != "keyed" || got.Keys < 4 {
			t.Fatalf("keyed tick = %+v, want the keyed path over at least the four dirty keys", got)
		}
		h.assertParity("several dirty keys on a hot triple")
	})
	t.Run("advisory-only key with no CVE", func(t *testing.T) {
		h.write(scopeA, scopeA+"/g1",
			keyedFinding("CVE-2026-0001", "pkg:npm/a", 20), keyedFinding("CVE-2026-0002", "pkg:npm/a", 5),
			keyedAdvisoryFinding("GHSA-9805-aaaa", "pkg:npm/adv", 30))
		h.keyed()
		h.assertParity("advisory-only")
	})
	t.Run("generation flip with a losing candidate that changes source_count", func(t *testing.T) {
		h.addGeneration(scopeB, scopeB+"/g2", "pending")
		h.write(scopeB, scopeB+"/g2", keyedFinding("CVE-2026-0077", "pkg:npm/b-only", 1))
		h.activate(scopeB, scopeB+"/g1", scopeB+"/g2")
		h.keyed()
		h.assertParity("generation activation flip")
	})
	t.Run("operator override and its removal", func(t *testing.T) {
		h.seedOperator("op-g2", "op-g1", map[string]map[string]any{
			"op-real": {
				"cve_id": "CVE-2026-0002", "package_id": "pkg:npm/a", "impact_status": "affected_exact",
				"observed_version": "1.0.0", "repository_id": "repo-9805", "suppression_state": "accepted_risk",
				"suppression": map[string]any{"expires_at": "2099-01-01T00:00:00Z"},
			},
			"op-noise": {"cve_id": "CVE-2026-0999", "suppression_state": "accepted_risk", "repository_id": "repo-9805"},
		})
		h.keyed()
		h.assertParity("operator override")
		h.seedOperator("op-g3", "op-g2", map[string]map[string]any{
			"op-noise": {"cve_id": "CVE-2026-0999", "suppression_state": "accepted_risk", "repository_id": "repo-9805"},
		})
		h.keyed()
		h.assertParity("operator override removed")
	})
	t.Run("a scope with no changes writes nothing", func(t *testing.T) {
		got := h.keyed()
		if got.Inserted != 0 || got.Updated != 0 || got.Deleted != 0 || got.Identifiers != 0 {
			t.Fatalf("an idle keyed tick = %+v, want zero identifiers and zero writes", got)
		}
		if wm := winnersLiveWatermark(t, h.ctx, h.db); !wm.Equal(h.clock) {
			t.Fatalf("watermark after an idle keyed tick = %v, want it stamped at %v", wm, h.clock)
		}
	})
}

// TestSupplyChainImpactWinnersResweepRandomInterleavingMatchesRecomputeLive is
// the property check behind the fixed case table (#9805 review F3, N4). A seeded
// random sequence of four production operation classes runs over several scopes
// that contend for the same keys: a same-generation rewrite that drops and
// revives findings, a retract-only pass (the writer with no findings), a
// generation flip through the Ack statement, and the production retirement
// fence plus purge with the real foreign keys. The purge always takes a scope
// that owns a stored winner when one exists, so every seed removes a winner. A
// keyed tick runs after every one to three operations, and the table must equal
// a fresh recompute each time. The Fail reconcile, the scanner tables and the
// operator scope are covered by the trigger case table, not by this sequence.
// The seeds are fixed, so a failure names the seed and the step that reproduce it.
func TestSupplyChainImpactWinnersResweepRandomInterleavingMatchesRecomputeLive(t *testing.T) {
	cves := []string{"CVE-2026-3001", "CVE-2026-3002", "CVE-2026-3003", "CVE-2026-3004"}
	pkgs := []string{"pkg:npm/p", "pkg:npm/q"}
	randomFindings := func(rng *rand.Rand) []supplychaincore.SupplyChainImpactFinding {
		var out []supplychaincore.SupplyChainImpactFinding
		for _, cve := range cves {
			for _, pkg := range pkgs {
				if rng.Intn(10) < 6 {
					out = append(out, keyedFinding(cve, pkg, 1+rng.Intn(9)))
				}
			}
		}
		return out
	}
	for _, seed := range []int64{1, 2, 3, 4} {
		t.Run(fmt.Sprintf("seed %d", seed), func(t *testing.T) {
			h := newKeyedHarness(t, "#9805 random interleaving proof")
			rng := rand.New(rand.NewSource(seed))
			type scopeState struct {
				id         string
				generation int
			}
			next := 0
			newScope := func() *scopeState {
				next++
				scope := &scopeState{id: fmt.Sprintf("vuln-intel://9805/rand%d-%d", seed, next), generation: 1}
				h.seedScope(scope.id, fmt.Sprintf("%s/g%d", scope.id, scope.generation))
				h.write(scope.id, fmt.Sprintf("%s/g%d", scope.id, scope.generation), randomFindings(rng)...)
				return scope
			}
			scopes := []*scopeState{newScope(), newScope(), newScope()}
			h.keyed()
			h.assertParity("seed")
			for step := 1; step <= 12; step++ {
				for ops := 1 + rng.Intn(3); ops > 0; ops-- {
					i := rng.Intn(len(scopes))
					scope := scopes[i]
					gen := fmt.Sprintf("%s/g%d", scope.id, scope.generation)
					switch rng.Intn(4) {
					case 0:
						h.write(scope.id, gen, randomFindings(rng)...)
					case 1:
						scope.generation++
						newGen := fmt.Sprintf("%s/g%d", scope.id, scope.generation)
						h.addGeneration(scope.id, newGen, "pending")
						h.write(scope.id, newGen, randomFindings(rng)...)
						h.activate(scope.id, gen, newGen)
					case 2:
						h.write(scope.id, gen)
					default:
						// Retire a scope that owns a stored winner, so the purge
						// removes a winner. The draw is taken either way.
						pick := rng.Intn(len(scopes))
						if owner := winnerOwnerIndex(h, scopes, func(s *scopeState) string { return s.id }, pick); owner >= 0 {
							i, scope = owner, scopes[owner]
						}
						retireScope(h, scope.id, fmt.Sprintf("%d-%d-%d", seed, step, ops))
						scopes[i] = newScope()
					}
				}
				h.keyed()
				h.assertParity(fmt.Sprintf("seed %d step %d", seed, step))
			}
			if audit, err := h.store.AuditWinners(h.ctx); err != nil || audit.Repairs != 0 {
				t.Fatalf("seed %d: audit = %+v, %v; want zero repairs: some operation left a capture hole", seed, audit, err)
			}
		})
	}
}

// winnerOwnerIndex returns the index of the pick-th scope (modulo the owners)
// that owns a stored winner, or -1 when none does.
func winnerOwnerIndex[T any](h *keyedHarness, scopes []T, id func(T) string, pick int) int {
	h.t.Helper()
	var owners []int
	for i, scope := range scopes {
		if h.count(`SELECT count(*) FROM supply_chain_impact_canonical_winners WHERE winner_scope_id = $1`, id(scope)) > 0 {
			owners = append(owners, i)
		}
	}
	if len(owners) == 0 {
		return -1
	}
	return owners[pick%len(owners)]
}

// retireScope runs the production retirement sequence for one scope: the fence
// supersedes its generations and nulls the pointer, then the purge deletes the
// scope row and the foreign keys cascade its facts. tag keeps the retirement id
// and idempotency key unique within a test.
func retireScope(h *keyedHarness, scope, tag string) {
	h.t.Helper()
	fenced, err := postgres.NewRetirementFencer(postgres.SQLDB{DB: h.db}, postgres.SQLDB{DB: h.db}).
		FenceRepository(h.ctx, postgres.FenceRequest{
			Retirements:        []postgres.FenceRetirement{{RetirementID: "rr_9805_" + tag, RepoID: scope, ScopeID: scope}},
			ReasonCode:         postgres.RetirementReasonOperatorRetired,
			ReasonHash:         "sha256:9805-reason",
			ActorClass:         "operator",
			ActorIDHash:        "sha256:9805-actor",
			IdempotencyKeyHash: "sha256:9805-key-" + tag,
		})
	if err != nil {
		h.t.Fatalf("FenceRepository %s: %v", scope, err)
	}
	purged, err := postgres.NewRetirementPurger(postgres.SQLDB{DB: h.db}, postgres.SQLDB{DB: h.db}).
		PurgeRetired(h.ctx, postgres.PurgeRequest{RepoIDs: fenced.RepoIDs})
	if err != nil || len(purged.Completed) != 1 || len(purged.Blocked) != 0 {
		h.t.Fatalf("PurgeRetired %s = %+v, %v; want the repository completed and nothing blocked", scope, purged, err)
	}
}
