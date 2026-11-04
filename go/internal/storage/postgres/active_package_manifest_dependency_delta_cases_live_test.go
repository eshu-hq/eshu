// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9991 disposable PostgreSQL proof of the delta-active manifest dependency read overlay rules, through the store and the supply-chain impact loader: delta wins per stable key, deleted manifest paths and tombstones remove base pins, chained deltas overlay only the newest delta on the newest activated full, a pruned, missing, legacy, or mismatched base keeps the delta-only rows and is counted, and a full-active scope is unchanged; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// TestListActivePackageManifestDependencyFactsDeltaOverridesBasePinLive
// isolates the stable-key rule: the delta's repository fact names no changed
// path, so only the shared stable key removes the base alpha row.
func TestListActivePackageManifestDependencyFactsDeltaOverridesBasePinLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	seedDeltaManifestBase(f, "scope-a", "full-a")
	f.generation("scope-a", deltaManifestGeneration{id: "delta-a", isDelta: true, commit: "c1", baseline: "c0"})
	f.deltaRepository("scope-a", "delta-a", nil, nil)
	f.pin("scope-a", "delta-a", deltaManifestPin{
		factID: "delta-alpha", key: "content_entity:scope-a:alpha", name: "alpha", path: "web/package.json", version: "2",
	})
	f.activate("scope-a", "delta-a")

	got, versions := f.load([]string{"npm"}, []string{"alpha", "bravo"})
	assertDeltaManifestIDs(t, got, []string{"delta-alpha", "full-bravo"})
	if versions["delta-alpha"] != "2" {
		t.Fatalf("delta-alpha version = %q, want 2", versions["delta-alpha"])
	}
	assertDeltaManifestOutcomes(t, f.unionOutcomes(), map[string]int64{"union_base_found": 1})
}

// TestListActivePackageManifestDependencyFactsDeltaDeletesManifestPathLive
// deletes api/package.json in the delta: the delta carries no manifest rows
// at all, so the scope reaches the overlay only through its base pins, and
// bravo must not be resurrected.
func TestListActivePackageManifestDependencyFactsDeltaDeletesManifestPathLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	seedDeltaManifestBase(f, "scope-a", "full-a")
	f.generation("scope-a", deltaManifestGeneration{id: "delta-a", isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0"})
	f.deltaRepository("scope-a", "delta-a", nil, []string{"api/package.json"})
	f.activate("scope-a", "delta-a")

	got, _ := f.load([]string{"npm"}, []string{"alpha", "bravo"})
	assertDeltaManifestIDs(t, got, []string{"full-alpha"})
	assertDeltaManifestOutcomes(t, f.unionOutcomes(), map[string]int64{"union_base_found": 1})
}

// TestListActivePackageManifestDependencyFactsDeltaTombstonesPinLive removes
// bravo through a delta tombstone on its stable key. The active read never
// returns tombstones, so the overlay must read them on its own.
func TestListActivePackageManifestDependencyFactsDeltaTombstonesPinLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	seedDeltaManifestBase(f, "scope-a", "full-a")
	f.generation("scope-a", deltaManifestGeneration{id: "delta-a", isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0"})
	f.deltaRepository("scope-a", "delta-a", nil, nil)
	f.pin("scope-a", "delta-a", deltaManifestPin{
		factID: "delta-bravo-tombstone", key: "content_entity:scope-a:bravo", name: "bravo", path: "api/package.json", tombstone: true,
	})
	f.activate("scope-a", "delta-a")

	got, _ := f.load([]string{"npm"}, []string{"alpha", "bravo"})
	assertDeltaManifestIDs(t, got, []string{"full-alpha"})
	assertDeltaManifestOutcomes(t, f.unionOutcomes(), map[string]int64{"union_base_found": 1})
}

// TestListActivePackageManifestDependencyFactsChainedDeltasLive overlays the
// newest delta of a chain on the newest activated full at its union-base
// commit. An older full at another commit and the intermediate delta stay
// out. A full-active neighbor scope reads unchanged in the same call.
func TestListActivePackageManifestDependencyFactsChainedDeltasLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	f.scope("scope-a")
	f.generation("scope-a", deltaManifestGeneration{id: "full-old", commit: "c-old"})
	f.pin("scope-a", "full-old", deltaManifestPin{factID: "old-echo", key: "content_entity:scope-a:echo", name: "echo", path: "old/package.json"})
	f.activate("scope-a", "full-old")
	f.generation("scope-a", deltaManifestGeneration{id: "full-a", commit: "c0"})
	f.pin("scope-a", "full-a", deltaManifestPin{factID: "full-alpha", key: "content_entity:scope-a:alpha", name: "alpha", path: "web/package.json"})
	f.pin("scope-a", "full-a", deltaManifestPin{factID: "full-bravo", key: "content_entity:scope-a:bravo", name: "bravo", path: "api/package.json"})
	f.pin("scope-a", "full-a", deltaManifestPin{factID: "full-charlie", key: "content_entity:scope-a:charlie", name: "charlie", path: "lib/package.json"})
	f.activate("scope-a", "full-a")
	f.generation("scope-a", deltaManifestGeneration{id: "delta-1", isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0"})
	f.deltaRepository("scope-a", "delta-1", []string{"web/package.json"}, nil)
	f.pin("scope-a", "delta-1", deltaManifestPin{factID: "d1-alpha", key: "content_entity:scope-a:alpha", name: "alpha", path: "web/package.json"})
	f.activate("scope-a", "delta-1")
	f.generation("scope-a", deltaManifestGeneration{id: "delta-2", isDelta: true, commit: "c2", baseline: "c1", unionBase: "c0"})
	f.deltaRepository("scope-a", "delta-2", []string{"web/package.json", "api/package.json"}, nil)
	f.pin("scope-a", "delta-2", deltaManifestPin{factID: "d2-alpha", key: "content_entity:scope-a:alpha", name: "alpha", path: "web/package.json"})
	f.pin("scope-a", "delta-2", deltaManifestPin{factID: "d2-bravo", key: "content_entity:scope-a:bravo", name: "bravo", path: "api/package.json"})
	f.activate("scope-a", "delta-2")

	f.scope("scope-b")
	f.generation("scope-b", deltaManifestGeneration{id: "full-b", commit: "b0"})
	f.pin("scope-b", "full-b", deltaManifestPin{factID: "b-alpha", key: "content_entity:scope-b:alpha", name: "alpha", path: "package.json"})
	f.activate("scope-b", "full-b")

	got, _ := f.load([]string{"npm"}, []string{"alpha", "bravo", "charlie", "echo"})
	assertDeltaManifestIDs(t, got, []string{"b-alpha", "d2-alpha", "d2-bravo", "full-charlie"})
	assertDeltaManifestOutcomes(t, f.unionOutcomes(), map[string]int64{"union_base_found": 1})
}

// TestListActivePackageManifestDependencyFactsDeltaBaseUnavailableLive keeps
// today's delta-only rows when the base cannot be trusted and counts why:
// scope-a's superseded base carries a retention prune event, and scope-b's
// union-base commit has no projected full at all.
func TestListActivePackageManifestDependencyFactsDeltaBaseUnavailableLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	seedDeltaManifestBase(f, "scope-a", "full-a")
	f.generation("scope-a", deltaManifestGeneration{id: "delta-a", isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0"})
	f.deltaRepository("scope-a", "delta-a", []string{"web/package.json"}, nil)
	f.pin("scope-a", "delta-a", deltaManifestPin{factID: "delta-a-alpha", key: "content_entity:scope-a:alpha", name: "alpha", path: "web/package.json"})
	f.activate("scope-a", "delta-a")
	f.exec("seed retention event", `
INSERT INTO generation_retention_events
    (event_id, scope_id_hash, generation_id_hash, scope_class, policy_scope,
     policy_revision, reason, row_counts, pruned_at)
VALUES ('event-1', $1, $2, 'repository', 'default', 'r1', 'superseded', '{}'::jsonb, clock_timestamp())`,
		retentionHashID("scope", "scope-a"), retentionHashID("generation", "full-a"))

	f.scope("scope-b")
	f.generation("scope-b", deltaManifestGeneration{id: "full-b", commit: "b0"})
	f.pin("scope-b", "full-b", deltaManifestPin{factID: "b-full-bravo", key: "content_entity:scope-b:bravo", name: "bravo", path: "api/package.json"})
	f.activate("scope-b", "full-b")
	f.generation("scope-b", deltaManifestGeneration{id: "delta-b", isDelta: true, commit: "b2", baseline: "b1", unionBase: "b1"})
	f.deltaRepository("scope-b", "delta-b", []string{"web/package.json"}, nil)
	f.pin("scope-b", "delta-b", deltaManifestPin{factID: "delta-b-alpha", key: "content_entity:scope-b:alpha", name: "alpha", path: "web/package.json"})
	f.activate("scope-b", "delta-b")

	got, _ := f.load([]string{"npm"}, []string{"alpha", "bravo"})
	assertDeltaManifestIDs(t, got, []string{"delta-a-alpha", "delta-b-alpha"})
	assertDeltaManifestOutcomes(t, f.unionOutcomes(), map[string]int64{"union_base_missing": 2})
	warnings := f.warnings("manifest dependency read delta base missing")
	if len(warnings) != 2 || !strings.Contains(strings.Join(warnings, "\n"), "delta_union_base_commit_sha=b1") {
		t.Fatalf("base-missing WARN lines = %q, want one per refused scope with its commits", warnings)
	}
}

// TestListActivePackageManifestDependencyFactsDeltaWithoutUnionLive keeps
// today's delta-only rows for the two deltas the projector does not union,
// and counts them: scope-a is a legacy delta with no recorded baseline, and
// scope-b resolves a base but carries no delta_generation repository fact.
func TestListActivePackageManifestDependencyFactsDeltaWithoutUnionLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	seedDeltaManifestBase(f, "scope-a", "full-a")
	f.generation("scope-a", deltaManifestGeneration{id: "delta-a", isDelta: true, commit: "c1"})
	f.pin("scope-a", "delta-a", deltaManifestPin{factID: "delta-a-alpha", key: "content_entity:scope-a:alpha", name: "alpha", path: "web/package.json"})
	f.activate("scope-a", "delta-a")

	f.scope("scope-b")
	f.generation("scope-b", deltaManifestGeneration{id: "full-b", commit: "b0"})
	f.pin("scope-b", "full-b", deltaManifestPin{factID: "b-full-bravo", key: "content_entity:scope-b:bravo", name: "bravo", path: "api/package.json"})
	f.activate("scope-b", "full-b")
	f.generation("scope-b", deltaManifestGeneration{id: "delta-b", isDelta: true, commit: "b1", baseline: "b0", unionBase: "b0"})
	f.pin("scope-b", "delta-b", deltaManifestPin{factID: "delta-b-alpha", key: "content_entity:scope-b:alpha", name: "alpha", path: "web/package.json"})
	f.activate("scope-b", "delta-b")

	got, _ := f.load([]string{"npm"}, []string{"alpha", "bravo"})
	assertDeltaManifestIDs(t, got, []string{"delta-a-alpha", "delta-b-alpha"})
	assertDeltaManifestOutcomes(t, f.unionOutcomes(), map[string]int64{
		"union_no_baseline": 1, "union_scope_mismatch": 1,
	})
	warnings := f.warnings("manifest dependency read delta carries no delta_generation repository fact")
	if len(warnings) != 1 || !strings.Contains(warnings[0], "generation_id=delta-b") {
		t.Fatalf("scope-mismatch WARN lines = %q, want one for delta-b", warnings)
	}
}

// TestListActivePackageManifestDependencyFactsDeltaMavenFamilyLive runs the
// overlay through the Maven-family query text: the base's untouched pom.xml
// pin stays visible under a delta that changed another file.
func TestListActivePackageManifestDependencyFactsDeltaMavenFamilyLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	f.scope("scope-a")
	f.generation("scope-a", deltaManifestGeneration{id: "full-a", commit: "c0"})
	f.pin("scope-a", "full-a", deltaManifestPin{
		factID: "full-maven", key: "content_entity:scope-a:maven", name: "org.example:core", path: "pom.xml", packageManager: "maven",
	})
	f.activate("scope-a", "full-a")
	f.generation("scope-a", deltaManifestGeneration{id: "delta-a", isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0"})
	f.deltaRepository("scope-a", "delta-a", []string{"README.md"}, nil)
	f.activate("scope-a", "delta-a")

	got, _ := f.load([]string{"maven"}, []string{"org.example:core"})
	assertDeltaManifestIDs(t, got, []string{"full-maven"})
	assertDeltaManifestOutcomes(t, f.unionOutcomes(), map[string]int64{"union_base_found": 1})
}

// TestListActivePackageManifestDependencyFactsFullActiveScopeUnchangedLive is
// the no-change guard: a full-active scope returns its active rows only, an
// older superseded full stays out, and no union decision is counted. It
// passes before and after the fix by design.
func TestListActivePackageManifestDependencyFactsFullActiveScopeUnchangedLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	seedDeltaManifestBase(f, "scope-a", "full-a")
	f.generation("scope-a", deltaManifestGeneration{id: "full-a2", commit: "c1"})
	f.pin("scope-a", "full-a2", deltaManifestPin{factID: "full2-alpha", key: "content_entity:scope-a:alpha", name: "alpha", path: "web/package.json"})
	f.activate("scope-a", "full-a2")

	got, _ := f.load([]string{"npm"}, []string{"alpha", "bravo"})
	assertDeltaManifestIDs(t, got, []string{"full2-alpha"})
	assertDeltaManifestOutcomes(t, f.unionOutcomes(), map[string]int64{})
}

// readManifestThroughImpactLoader seeds scope-a (a delta over a resolvable
// base) and scope-b (a delta whose union base was never projected), then runs
// the manifest read inside the supply-chain impact loader's static expansion,
// the path #9991 was filed for. It reports whether the loader took the
// engaged (snapshot) branch: only that branch passes public advisories.
func readManifestThroughImpactLoader(t *testing.T, f *deltaManifestFixture) ([]string, bool) {
	t.Helper()
	seedDeltaManifestBase(f, "scope-a", "full-a")
	f.generation("scope-a", deltaManifestGeneration{id: "delta-a", isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0"})
	f.deltaRepository("scope-a", "delta-a", []string{"web/package.json"}, nil)
	f.pin("scope-a", "delta-a", deltaManifestPin{factID: "delta-alpha", key: "content_entity:scope-a:alpha", name: "alpha", path: "web/package.json"})
	f.activate("scope-a", "delta-a")
	f.scope("scope-b")
	f.generation("scope-b", deltaManifestGeneration{id: "delta-b", isDelta: true, commit: "b2", baseline: "b1", unionBase: "b1"})
	f.deltaRepository("scope-b", "delta-b", []string{"web/package.json"}, nil)
	f.pin("scope-b", "delta-b", deltaManifestPin{factID: "delta-b-alpha", key: "content_entity:scope-b:alpha", name: "alpha", path: "web/package.json"})
	f.activate("scope-b", "delta-b")

	loader := NewAuthorizedSupplyChainImpactEvidenceLoaderWithTelemetry(SQLDB{DB: f.database}, f.store.Instruments(), f.store.Logger())
	var got []string
	engaged := false
	if _, err := loader.ListAuthorizedSupplyChainImpactEvidence(f.ctx,
		reducercontract.Intent{ScopeID: "scope-a", GenerationID: "delta-a", Domain: reducercontract.DomainSupplyChainImpact},
		func(ctx context.Context, factLoader factload.FactLoader, public []facts.Envelope) (supplychaincore.AuthorizedStaticSupplyChainImpactLoad, error) {
			engaged = public != nil
			store, ok := factLoader.(*FactStore)
			if !ok {
				return supplychaincore.AuthorizedStaticSupplyChainImpactLoad{}, fmt.Errorf("fact loader is %T, want *FactStore", factLoader)
			}
			loaded, err := store.ListActivePackageManifestDependencyFacts(ctx, []string{"npm"}, []string{"alpha", "bravo"})
			for _, envelope := range loaded {
				got = append(got, envelope.FactID)
			}
			return supplychaincore.AuthorizedStaticSupplyChainImpactLoad{Complete: true}, err
		},
	); err != nil {
		t.Fatalf("ListAuthorizedSupplyChainImpactEvidence(): %v", err)
	}
	slices.Sort(got)
	return got, engaged
}

// assertImpactLoaderManifestTelemetry checks the rows, the union counter,
// and the base-missing WARN of readManifestThroughImpactLoader.
func assertImpactLoaderManifestTelemetry(t *testing.T, f *deltaManifestFixture, got []string) {
	t.Helper()
	assertDeltaManifestIDs(t, got, []string{"delta-alpha", "delta-b-alpha", "full-bravo"})
	assertDeltaManifestOutcomes(t, f.unionOutcomes(), map[string]int64{"union_base_found": 1, "union_base_missing": 1})
	if warnings := f.warnings("manifest dependency read delta base missing"); len(warnings) != 1 ||
		!strings.Contains(warnings[0], "generation_id=delta-b") {
		t.Fatalf("base-missing WARN lines = %q, want one for delta-b", warnings)
	}
}

// TestAuthorizedImpactLoaderCountsManifestDeltaUnionLive proves the
// delta-union counter and WARN record through the impact loader's
// disengaged store (#9991 F2): no role policy is installed.
func TestAuthorizedImpactLoaderCountsManifestDeltaUnionLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	got, engaged := readManifestThroughImpactLoader(t, f)
	if engaged {
		t.Fatal("loader took the engaged branch without role policy")
	}
	assertImpactLoaderManifestTelemetry(t, f, got)
}

// TestAuthorizedImpactLoaderCountsManifestDeltaUnionEngagedLive proves the
// same through the engaged branch's snapshot store: with role policy
// installed the loader reads inside its repeatable-read snapshot clone,
// which must inherit the loader's instruments and logger (#9991 G1).
func TestAuthorizedImpactLoaderCountsManifestDeltaUnionEngagedLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	f.exec("install role policy", `
INSERT INTO eshu_security.role_policy_state
    (policy_revision, database_oid, database_identity, system_identifier,
     inventory_version, owner_role_oid, authority_epoch, active)
SELECT 999101, (SELECT oid FROM pg_database WHERE datname = current_database()),
       '11111111-2222-4333-8444-555555555555',
       (SELECT system_identifier FROM pg_control_system()), 1,
       (SELECT oid FROM pg_roles WHERE rolname = current_user), 1, TRUE`)
	got, engaged := readManifestThroughImpactLoader(t, f)
	if !engaged {
		t.Fatal("loader took the disengaged branch with role policy installed")
	}
	assertImpactLoaderManifestTelemetry(t, f, got)
}
