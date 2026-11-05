// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10003 and #10005 disposable PostgreSQL proof that the Crossplane SATISFIED_BY reads overlay a delta-active git scope on its #7797 base full generation: an XRD in an untouched file stays visible, the handler keeps the edge of an unchanged claim beside a changed, added, or deleted one, a delta touching only the XRD loses no edge, a replay and a full generation write the same edges, and a missing base keeps the delta-only rows and is counted; reads ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DISPOSABLE ESHU_PACKAGE_CONSUMPTION_SCOPE_PROOF_DSN

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossplane"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// crossplaneDeltaClaimKind is the claim kind of every XRD identity in these
// fixtures; each case gets its own group, so the cases share one database
// without one case's XRD making another case's claims ambiguous.
const crossplaneDeltaClaimKind = "DeltaQueue"

// crossplaneDeltaEntity is one K8sResource claim or CrossplaneXRD
// content_entity row to seed. The stable key is content_entity:<entity id>,
// the key gitcontent.ContentEntityFactEnvelope emits, so a delta that
// re-emits an entity overrides its base row by key, and a delta that touches
// its file removes the base row by path.
type crossplaneDeltaEntity struct {
	group     string
	factID    string
	entityID  string
	path      string
	xrd       bool
	tombstone bool
}

func (f *deltaManifestFixture) crossplaneEntity(scopeID, generationID string, entity crossplaneDeltaEntity) {
	f.t.Helper()
	entityType := "K8sResource"
	metadata := `jsonb_build_object('api_version', $8::text || '/v1alpha1', 'kind', $9::text)`
	if entity.xrd {
		entityType = "CrossplaneXRD"
		metadata = `jsonb_build_object('group', $8::text, 'claim_kind', $9::text)`
	}
	f.exec("seed crossplane entity "+entity.factID, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
     source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, 'content_entity', 'content_entity:' || $4,
        'git', 'content_entity:' || $4, clock_timestamp(), clock_timestamp(), $5,
        jsonb_build_object(
            'repo_id', $2::text,
            'entity_id', $4::text,
            'relative_path', $6::text,
            'entity_type', $7::text,
            'entity_name', $4::text,
            'entity_metadata', `+metadata+`))`,
		entity.factID, scopeID, generationID, entity.entityID, entity.tombstone, entity.path, entityType,
		entity.group, crossplaneDeltaClaimKind)
}

// crossplaneDeltaSpy records the handler's retracts and edge writes.
type crossplaneDeltaSpy struct {
	retracts [][]string
	edges    []string
}

func (s *crossplaneDeltaSpy) WriteCrossplaneSatisfiedByEdges(_ context.Context, rows []map[string]any, _, _, _ string) error {
	for _, row := range rows {
		s.edges = append(s.edges, row["claim_uid"].(string)+"->"+row["xrd_uid"].(string))
	}
	return nil
}

func (s *crossplaneDeltaSpy) RetractCrossplaneSatisfiedByEdges(_ context.Context, scopeIDs []string, _, _ string) error {
	s.retracts = append(s.retracts, slices.Clone(scopeIDs))
	return nil
}

// handle runs the production handler over the store for one intent and
// returns the sorted edges it wrote, failing unless it retracted exactly the
// intent's scope once.
func (f *deltaManifestFixture) handle(scopeID, generationID string, attempt int) []string {
	f.t.Helper()
	spy := &crossplaneDeltaSpy{}
	handler := crossplane.CrossplaneSatisfiedByMaterializationHandler{FactLoader: f.store, EdgeWriter: spy}
	if _, err := handler.Handle(f.ctx, reducercontract.Intent{
		IntentID: "intent-" + generationID, ScopeID: scopeID, GenerationID: generationID,
		Domain: reducercontract.DomainCrossplaneSatisfiedByMaterialization, AttemptCount: attempt,
	}); err != nil {
		f.t.Fatalf("Handle(%s): %v", generationID, err)
	}
	if len(spy.retracts) != 1 || !slices.Equal(spy.retracts[0], []string{scopeID}) {
		f.t.Fatalf("retracts = %v, want exactly [[%s]]", spy.retracts, scopeID)
	}
	slices.Sort(spy.edges)
	return spy.edges
}

// crossplaneOutcomes returns the Crossplane read delta-union counter by
// "read/outcome".
func (f *deltaManifestFixture) crossplaneOutcomes() map[string]int64 {
	f.t.Helper()
	var rm metricdata.ResourceMetrics
	if err := f.reader.Collect(context.Background(), &rm); err != nil {
		f.t.Fatalf("collect: %v", err)
	}
	outcomes := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_crossplane_satisfied_by_read_delta_union_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				f.t.Fatalf("%s is %T, want an int64 sum", m.Name, m.Data)
			}
			for _, dp := range sum.DataPoints {
				read, _ := dp.Attributes.Value(telemetry.MetricDimensionRead)
				outcome, _ := dp.Attributes.Value(telemetry.MetricDimensionOutcome)
				outcomes[read.AsString()+"/"+outcome.AsString()] += dp.Value
			}
		}
	}
	return outcomes
}

// seedCrossplaneFull seeds scopeID with an activated full generation at
// commit c0 holding the given entities.
func seedCrossplaneFull(f *deltaManifestFixture, scopeID string, entities ...crossplaneDeltaEntity) {
	f.t.Helper()
	f.scope(scopeID)
	f.generation(scopeID, deltaManifestGeneration{id: scopeID + "-full", commit: "c0"})
	for _, entity := range entities {
		f.crossplaneEntity(scopeID, scopeID+"-full", entity)
	}
	f.activate(scopeID, scopeID+"-full")
}

// seedCrossplaneDelta seeds and activates a delta at c1 over the c0 base that
// changes changed, deletes deleted, and carries the given entities.
func seedCrossplaneDelta(f *deltaManifestFixture, scopeID string, changed, deleted []string, entities ...crossplaneDeltaEntity) string {
	f.t.Helper()
	deltaID := scopeID + "-delta"
	f.generation(scopeID, deltaManifestGeneration{id: deltaID, isDelta: true, commit: "c1", baseline: "c0", unionBase: "c0"})
	f.deltaRepository(scopeID, deltaID, changed, deleted)
	for _, entity := range entities {
		f.crossplaneEntity(scopeID, deltaID, entity)
	}
	f.activate(scopeID, deltaID)
	return deltaID
}

// crossplaneDeltaCase names one case's scopes, entities, and XRD identity:
// every id carries the case prefix.
type crossplaneDeltaCase struct{ prefix string }

func (c crossplaneDeltaCase) scope(name string) string { return c.prefix + "-" + name }

func (c crossplaneDeltaCase) xrd(factID string) crossplaneDeltaEntity {
	return crossplaneDeltaEntity{
		group: c.prefix + ".acme.internal", factID: c.prefix + "-" + factID,
		entityID: c.prefix + "-xrd-1", path: "deploy/xrd.yaml", xrd: true,
	}
}

func (c crossplaneDeltaCase) claim(factID, claim string) crossplaneDeltaEntity {
	return crossplaneDeltaEntity{
		group: c.prefix + ".acme.internal", factID: c.prefix + "-" + factID,
		entityID: c.prefix + "-" + claim, path: "deploy/" + claim + ".yaml",
	}
}

// edge names the SATISFIED_BY edge from one of the case's claims to its XRD.
func (c crossplaneDeltaCase) edge(claim string) string {
	return c.prefix + "-" + claim + "->" + c.prefix + "-xrd-1"
}

// outcomesSince returns the counter increments since before.
func outcomesSince(before, after map[string]int64) map[string]int64 {
	diff := map[string]int64{}
	for key, count := range after {
		if delta := count - before[key]; delta != 0 {
			diff[key] = delta
		}
	}
	return diff
}

func assertCrossplaneEdges(t *testing.T, got []string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("SATISFIED_BY edges = %v, want %v", got, want)
	}
}

func assertCrossplaneOutcomes(t *testing.T, got, want map[string]int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("crossplane delta union outcomes = %v, want %v", got, want)
	}
	for key, count := range want {
		if got[key] != count {
			t.Fatalf("crossplane delta union outcomes = %v, want %v", got, want)
		}
	}
}

// TestListActiveCrossplaneXRDFactsDeltaActiveScopeLive is the #10003 row 3
// regression at the reader. The platform scope's full generation holds the
// XRD in deploy/xrd.yaml; a delta that touches only deploy/other.yaml becomes
// active. Before the fix the read joined only the active generation and
// returned no XRD, so every claim resolving against it lost its edge.
func TestListActiveCrossplaneXRDFactsDeltaActiveScopeLive(t *testing.T) {
	f := openDeltaManifestFixture(t)
	c := crossplaneDeltaCase{prefix: "reader"}
	seedCrossplaneFull(f, "platform", c.xrd("platform-full-xrd"))
	seedCrossplaneDelta(f, "platform", []string{"deploy/other.yaml"}, nil)

	loaded, err := f.store.ListActiveCrossplaneXRDFacts(f.ctx)
	if err != nil {
		t.Fatalf("ListActiveCrossplaneXRDFacts(): %v", err)
	}
	ids := make([]string, 0, len(loaded))
	for _, env := range loaded {
		ids = append(ids, env.FactID)
	}
	if !slices.Equal(ids, []string{"reader-platform-full-xrd"}) {
		t.Fatalf("loaded XRD facts = %v, want [reader-platform-full-xrd] (the base XRD the delta never touched)", ids)
	}
	assertCrossplaneOutcomes(t, f.crossplaneOutcomes(), map[string]int64{"xrd/union_base_found": 1})
}

// TestCrossplaneSatisfiedByDeltaKeepsUnchangedEdgesLive is the combined
// #10003 row 3 and #10005 regression through the production handler and
// store. Each case seeds a full generation, activates a delta over it, runs
// the handler for the delta, and asserts the edges written after the
// handler's retract of the whole scope: an edge the delta did not change
// must survive.
func TestCrossplaneSatisfiedByDeltaKeepsUnchangedEdgesLive(t *testing.T) {
	// One database for every case (the bootstrap dominates the wall time);
	// each case owns its scopes and XRD identity, and its counter assertions
	// read increments only.
	f := openDeltaManifestFixtureWithin(t, 5*time.Minute)
	cases := []struct {
		name string
		body func(t *testing.T, c crossplaneDeltaCase)
	}{
		{"same-scope claim rewritten, XRD untouched", func(t *testing.T, c crossplaneDeltaCase) {
			// The golden dr-4 shape: before the fix the delta-active XRD
			// read returned nothing and the claim lost its edge.
			app := c.scope("app")
			seedCrossplaneFull(f, app, c.xrd("full-xrd"), c.claim("full-claim-1", "claim-1"))
			deltaID := seedCrossplaneDelta(f, app, []string{"deploy/claim-1.yaml"}, nil, c.claim("delta-claim-1", "claim-1"))

			before := f.crossplaneOutcomes()
			assertCrossplaneEdges(t, f.handle(app, deltaID, 1), c.edge("claim-1"))
			assertCrossplaneOutcomes(t, outcomesSince(before, f.crossplaneOutcomes()), map[string]int64{
				"claim/union_base_found": 1, "xrd/union_base_found": 1,
			})
		}},
		{"claim added beside an unchanged claim", func(t *testing.T, c crossplaneDeltaCase) {
			// #10005: before the fix the handler loaded the delta's claim-2
			// only and the retract deleted claim-1's edge.
			app := c.scope("app")
			seedCrossplaneFull(f, c.scope("platform"), c.xrd("platform-full-xrd"))
			seedCrossplaneFull(f, app, c.claim("full-claim-1", "claim-1"))
			deltaID := seedCrossplaneDelta(f, app, []string{"deploy/claim-2.yaml"}, nil, c.claim("delta-claim-2", "claim-2"))

			assertCrossplaneEdges(t, f.handle(app, deltaID, 1), c.edge("claim-1"), c.edge("claim-2"))
		}},
		{"claim changed beside an unchanged claim, then replayed", func(t *testing.T, c crossplaneDeltaCase) {
			app := c.scope("app")
			seedCrossplaneFull(f, c.scope("platform"), c.xrd("platform-full-xrd"))
			seedCrossplaneFull(f, app, c.claim("full-claim-1", "claim-1"), c.claim("full-claim-2", "claim-2"))
			deltaID := seedCrossplaneDelta(f, app, []string{"deploy/claim-2.yaml"}, nil, c.claim("delta-claim-2", "claim-2"))

			first := f.handle(app, deltaID, 1)
			assertCrossplaneEdges(t, first, c.edge("claim-1"), c.edge("claim-2"))
			// A retry (attempt 2 always retracts) and a replay converge on
			// the same edge set.
			assertCrossplaneEdges(t, f.handle(app, deltaID, 2), first...)
			assertCrossplaneEdges(t, f.handle(app, deltaID, 1), first...)
		}},
		{"claim deleted beside an unchanged claim", func(t *testing.T, c crossplaneDeltaCase) {
			app := c.scope("app")
			seedCrossplaneFull(f, c.scope("platform"), c.xrd("platform-full-xrd"))
			seedCrossplaneFull(f, app, c.claim("full-claim-1", "claim-1"), c.claim("full-claim-2", "claim-2"))
			deleted := c.claim("delta-claim-2", "claim-2")
			deleted.tombstone = true
			deltaID := seedCrossplaneDelta(f, app, nil, []string{"deploy/claim-2.yaml"}, deleted)

			assertCrossplaneEdges(t, f.handle(app, deltaID, 1), c.edge("claim-1"))
		}},
		{"delta touches only the XRD", func(t *testing.T, c crossplaneDeltaCase) {
			app := c.scope("app")
			seedCrossplaneFull(f, app, c.xrd("full-xrd"), c.claim("full-claim-1", "claim-1"), c.claim("full-claim-2", "claim-2"))
			deltaID := seedCrossplaneDelta(f, app, []string{"deploy/xrd.yaml"}, nil, c.xrd("delta-xrd"))

			assertCrossplaneEdges(t, f.handle(app, deltaID, 1), c.edge("claim-1"), c.edge("claim-2"))
		}},
		{"full generation is unchanged", func(t *testing.T, c crossplaneDeltaCase) {
			// Only full generations in this case; the earlier cases' deltas
			// stay active, so the xrd leg still reaches them, but this
			// case's own claim leg makes no overlay decision.
			app := c.scope("app")
			seedCrossplaneFull(f, c.scope("platform"), c.xrd("platform-full-xrd"))
			seedCrossplaneFull(f, app, c.claim("full-claim-1", "claim-1"), c.claim("full-claim-2", "claim-2"))

			before := f.crossplaneOutcomes()
			assertCrossplaneEdges(t, f.handle(app, app+"-full", 1), c.edge("claim-1"), c.edge("claim-2"))
			if got := outcomesSince(before, f.crossplaneOutcomes()); got["claim/union_base_found"]+got["claim/union_base_missing"] != 0 {
				t.Fatalf("claim leg made an overlay decision for a full-active scope: %v", got)
			}
		}},
		{"missing base keeps the delta rows and is counted", func(t *testing.T, c crossplaneDeltaCase) {
			// The delta's baseline commit c0 has no activated full
			// generation: the read cannot overlay, keeps the delta's own
			// rows, and says so.
			app := c.scope("app")
			seedCrossplaneFull(f, c.scope("platform"), c.xrd("platform-full-xrd"))
			f.scope(app)
			f.generation(app, deltaManifestGeneration{id: app + "-full", commit: "c-other"})
			f.crossplaneEntity(app, app+"-full", c.claim("full-claim-1", "claim-1"))
			f.activate(app, app+"-full")
			deltaID := seedCrossplaneDelta(f, app, []string{"deploy/claim-2.yaml"}, nil, c.claim("delta-claim-2", "claim-2"))

			before := f.crossplaneOutcomes()
			warned := len(f.warnings("crossplane satisfied-by read delta base missing"))
			assertCrossplaneEdges(t, f.handle(app, deltaID, 1), c.edge("claim-2"))
			if got := outcomesSince(before, f.crossplaneOutcomes()); got["claim/union_base_missing"] != 1 {
				t.Fatalf("claim leg outcomes = %v, want one union_base_missing", got)
			}
			if got := len(f.warnings("crossplane satisfied-by read delta base missing")) - warned; got < 1 {
				t.Fatalf("base-missing WARN lines added = %d, want at least 1:\n%s", got, f.logs.String())
			}
		}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f.t = t
			tc.body(t, crossplaneDeltaCase{prefix: "case" + strconv.Itoa(i)})
		})
	}
}
