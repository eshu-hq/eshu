// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10094 disposable PostgreSQL differential for the Crossplane redrive discovery reshape: the shipped DISTINCT-branch text, its per-claim form, and the pre-#10094 text return identical active row sets, and the base texts derived from the shipped and per-claim forms return identical delta-base row sets, over duplicate claims, NULL and entity_kind payloads, tombstones, two-generation scopes, a ledger row, a legacy delta, a missing base, and every keyset page size; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN

import (
	"maps"
	"slices"
	"testing"
	"time"

	crossplanestore "github.com/eshu-hq/eshu/go/internal/storage/postgres/crossplane"
)

// collectCrossplaneRedriveTargets pages one discovery text to its end and
// returns scope -> generation, failing on a scope returned twice.
func (f *deltaManifestFixture) collectCrossplaneRedriveTargets(query, group string, pageSize int, baseGenerations []string) map[string]string {
	f.t.Helper()
	out := map[string]string{}
	page := crossplanestore.CrossplaneRedriveTargetPage{Group: group, ClaimKind: crossplaneDeltaClaimKind, XRDScopeID: "diff-xrd", Limit: pageSize}
	for {
		args := page.QueryArgs()
		if baseGenerations != nil {
			args = append(args, baseGenerations)
		}
		targets, err := crossplanestore.QueryCrossplaneRedriveTargetScopes(f.ctx, SQLDB{DB: f.database}, "differential", query, args...)
		if err != nil {
			f.t.Fatalf("page after %q: %v", page.AfterScopeID, err)
		}
		for _, target := range targets {
			if _, dup := out[target.ScopeID]; dup {
				f.t.Fatalf("scope %s returned twice (page size %d)", target.ScopeID, pageSize)
			}
			out[target.ScopeID] = target.GenerationID
		}
		if len(targets) < pageSize {
			return out
		}
		page.AfterScopeID = targets[len(targets)-1].ScopeID
	}
}

// TestCrossplaneRedriveDiscoveryReshapeDifferentialLive is the #10094
// accuracy proof for the DISTINCT-branch discovery text. Every text under
// comparison is derived from the shipped constant: its per-claim form, the
// pre-#10094 form without the generation join, and the base texts
// deriveDeltaOverlayBaseQuery derives from the shipped and per-claim forms.
// The output tuple is functionally dependent on scope_id (the primary key
// of ingestion_scopes), so collapsing claims to (scope, generation) before
// the joins cannot change the row set; this pins it on a corpus with the
// edge cases, for every keyset page size from one row to the whole set.
func TestCrossplaneRedriveDiscoveryReshapeDifferentialLive(t *testing.T) {
	f := openDeltaManifestFixtureWithin(t, 4*time.Minute)
	c := crossplaneDeltaCase{prefix: "diff"}
	group := c.prefix + ".acme.internal"
	claim := func(scopeID, factID string) crossplaneDeltaEntity {
		return crossplaneDeltaEntity{group: group, factID: factID, entityID: factID, path: "deploy/" + factID + ".yaml"}
	}

	// The XRD's own scope carries a matching claim: $3 excludes it.
	seedCrossplaneFull(f, "diff-xrd", c.xrd("xrd"), claim("diff-xrd", "diff-xrd-claim"))
	// Duplicate claims in one generation collapse to one target.
	seedCrossplaneFull(f, "diff-a", claim("diff-a", "diff-a-1"), claim("diff-a", "diff-a-2"), claim("diff-a", "diff-a-3"))
	// A tombstone, a NULL api_version, and an entity_kind-only claim.
	seedCrossplaneFull(f, "diff-b", claim("diff-b", "diff-b-1"))
	tomb := claim("diff-b", "diff-b-tomb")
	tomb.tombstone = true
	f.crossplaneEntity("diff-b", "diff-b-full", tomb)
	seedCrossplaneFull(f, "diff-null")
	f.exec("seed NULL api_version claim", `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('diff-null-1', 'diff-null', 'diff-null-full', 'content_entity', 'content_entity:diff-null-1', 'git', 'content_entity:diff-null-1', clock_timestamp(), clock_timestamp(), FALSE,
        jsonb_build_object('entity_type', 'K8sResource', 'entity_metadata', jsonb_build_object('api_version', NULL, 'kind', $1::text)))`, crossplaneDeltaClaimKind)
	seedCrossplaneFull(f, "diff-kind")
	f.exec("seed entity_kind claim", `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ('diff-kind-1', 'diff-kind', 'diff-kind-full', 'content_entity', 'content_entity:diff-kind-1', 'git', 'content_entity:diff-kind-1', clock_timestamp(), clock_timestamp(), FALSE,
        jsonb_build_object('entity_kind', 'K8sResource', 'entity_metadata', jsonb_build_object('api_version', $1::text || '/v1', 'kind', $2::text)))`, group, crossplaneDeltaClaimKind)
	// Claims only in the base of an untouched delta, and in both generations.
	seedCrossplaneFull(f, "diff-c", claim("diff-c", "diff-c-1"))
	seedCrossplaneDelta(f, "diff-c", []string{"deploy/other.yaml"}, nil)
	seedCrossplaneFull(f, "diff-d", claim("diff-d", "diff-d-1"))
	seedCrossplaneDelta(f, "diff-d", []string{"deploy/diff-d-2.yaml"}, nil, claim("diff-d", "diff-d-2"))
	// Claims in a superseded older full and the active full.
	f.scope("diff-e")
	f.generation("diff-e", deltaManifestGeneration{id: "diff-e-old", commit: "c-old"})
	f.crossplaneEntity("diff-e", "diff-e-old", claim("diff-e", "diff-e-old-1"))
	f.activate("diff-e", "diff-e-old")
	f.generation("diff-e", deltaManifestGeneration{id: "diff-e-full", commit: "c0"})
	f.crossplaneEntity("diff-e", "diff-e-full", claim("diff-e", "diff-e-1"))
	f.activate("diff-e", "diff-e-full")
	// A delta whose baseline commit has no activated full: no base.
	f.scope("diff-f")
	f.generation("diff-f", deltaManifestGeneration{id: "diff-f-full", commit: "c-other"})
	f.crossplaneEntity("diff-f", "diff-f-full", claim("diff-f", "diff-f-1"))
	f.activate("diff-f", "diff-f-full")
	seedCrossplaneDelta(f, "diff-f", []string{"deploy/other.yaml"}, nil)
	// A ledgered delta-active scope, and a legacy delta with no baseline.
	seedCrossplaneFull(f, "diff-g", claim("diff-g", "diff-g-1"))
	seedCrossplaneDelta(f, "diff-g", []string{"deploy/other.yaml"}, nil)
	f.exec("ledger diff-g", `
INSERT INTO crossplane_satisfied_by_redrive_target_ledger (target_scope_id, xrd_group, xrd_claim_kind, redriven_at)
VALUES ('diff-g', $1, $2, clock_timestamp())`, group, crossplaneDeltaClaimKind)
	seedCrossplaneFull(f, "diff-h", claim("diff-h", "diff-h-1"))
	f.generation("diff-h", deltaManifestGeneration{id: "diff-h-delta", isDelta: true, commit: "c1"})
	f.deltaRepository("diff-h", "diff-h-delta", []string{"deploy/other.yaml"}, nil)
	f.activate("diff-h", "diff-h-delta")
	f.exec("analyze", "ANALYZE fact_records, scope_generations, ingestion_scopes")

	shipped := crossplanestore.ListCrossplaneRedriveTargetScopesQuery
	perClaim, err := crossplaneRedrivePerClaimText(shipped)
	if err != nil {
		t.Fatal(err)
	}
	preFix, err := crossplaneRedrivePreFixText(perClaim)
	if err != nil {
		t.Fatal(err)
	}
	param := crossplanestore.CrossplaneRedriveDeltaBaseGenerationParam
	shippedBase, err := deriveDeltaOverlayBaseQuery(shipped, param)
	if err != nil {
		t.Fatal(err)
	}
	perClaimBase, err := deriveDeltaOverlayBaseQuery(perClaim, param)
	if err != nil {
		t.Fatal(err)
	}
	baseGenerations, err := f.store.CrossplaneRedriveDeltaBaseGenerations(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(baseGenerations)
	// diff-f has a baseline but no activated full generation at it, so it
	// contributes no candidate; the legacy diff-h delta has no baseline.
	if want := []string{"diff-c-full", "diff-d-full", "diff-g-full"}; !slices.Equal(baseGenerations, want) {
		t.Fatalf("delta base generations = %v, want %v", baseGenerations, want)
	}

	wantActive := map[string]string{
		"diff-a": "diff-a-full", "diff-b": "diff-b-full", "diff-kind": "diff-kind-full",
		"diff-d": "diff-d-delta", "diff-e": "diff-e-full",
	}
	wantBase := map[string]string{"diff-c": "diff-c-delta", "diff-d": "diff-d-delta"}
	for _, pageSize := range []int{1, 2, 3, 500} {
		for name, query := range map[string]string{"shipped": shipped, "per-claim": perClaim, "pre-#10094": preFix} {
			if got := f.collectCrossplaneRedriveTargets(query, group, pageSize, nil); !maps.Equal(got, wantActive) {
				t.Fatalf("%s active (page %d) = %v, want %v", name, pageSize, got, wantActive)
			}
		}
		for name, query := range map[string]string{"shipped-derived": shippedBase, "per-claim-derived": perClaimBase} {
			if got := f.collectCrossplaneRedriveTargets(query, group, pageSize, baseGenerations); !maps.Equal(got, wantBase) {
				t.Fatalf("%s base (page %d) = %v, want %v", name, pageSize, got, wantBase)
			}
		}
	}
}
