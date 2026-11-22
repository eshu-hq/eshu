// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11203 end-to-end proof on disposable PostgreSQL 18 that IngestionStore.CommitScopeGeneration refuses a whole generation whose fact id is held by another scope or another generation: nothing of the generation lands (no fact, no generation row, no projector work item), the owner's fact row and its relationship_reference_candidate_keys row stay byte-identical, the scope's prior generation stays active, the commit disposition reads refused with reason fact_id_owner_conflict, and the generation-outcome counter, the fact id scope conflict counter by reason, the skipped-refresh count, and one payload-free ERROR log all agree; reuses the #7978 bootstrapped proof schema; wall time logged not gated
//eshu:readiness:env ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ownerRefusalFile builds one eligible-for-reference-keys file fact.
func ownerRefusalFile(scopeID, generationID, factID string, at time.Time, tag string) facts.Envelope {
	return facts.Envelope{
		FactID: factID, ScopeID: scopeID, GenerationID: generationID, FactKind: "file",
		StableFactKey: "file:proof/11203:" + factID, ObservedAt: at,
		Payload: map[string]any{"relative_path": factID + ".go", "owner": tag},
	}
}

func (p *reprojectProof) ownerRefusalCommit(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	envelopes []facts.Envelope,
) error {
	stream := make(chan facts.Envelope, len(envelopes))
	for _, envelope := range envelopes {
		stream <- envelope
	}
	close(stream)
	return p.store.CommitScopeGeneration(ctx, scopeValue, generation, stream)
}

func (p *reprojectProof) ownerRefusalGeneration(scopeID, id string) scope.ScopeGeneration {
	generation := p.generation(id, "hint-"+id, "sha-"+id, "")
	generation.ScopeID = scopeID
	return generation
}

// queryString runs a one-value text query.
func (p *reprojectProof) queryString(query string, args ...any) string {
	p.t.Helper()
	var out string
	if err := p.db.QueryRowContext(context.Background(), query, args...).Scan(&out); err != nil {
		p.t.Fatalf("query %q: %v", query, err)
	}
	return out
}

// TestCommitScopeGenerationRefusesFactIDOwnerConflictLive is the #11203
// end-to-end proof for both collision reasons.
func TestCommitScopeGenerationRefusesFactIDOwnerConflictLive(t *testing.T) {
	for _, tc := range []struct {
		name       string
		otherScope bool
		reason     string
	}{
		{"another scope owns the fact id", true, telemetry.FactIDOwnerConflictReasonScope},
		{"another generation of the same scope owns the fact id", false, telemetry.FactIDOwnerConflictReasonGeneration},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := openReprojectProof(t)
			ctx := context.Background()
			telemetry.ResetSkippedRefreshCountForTesting()
			t.Cleanup(telemetry.ResetSkippedRefreshCountForTesting)

			reader := metric.NewManualReader()
			instruments, err := telemetry.NewInstruments(metric.NewMeterProvider(metric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			var logs bytes.Buffer
			p.store.Instruments = instruments
			p.store.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

			scopeA := p.scope
			scopeB := p.scope
			scopeB.ScopeID = "git-repository-scope:proof/11203-b"
			scopeB.PartitionKey = "proof/11203-b"
			victim := scopeB // the scope whose generation is refused
			if !tc.otherScope {
				victim = scopeA
			}

			// Scope A owns "collide" and its reference-key row.
			gA1 := p.ownerRefusalGeneration(scopeA.ScopeID, "gen-a1")
			if err := p.ownerRefusalCommit(ctx, scopeA, gA1, []facts.Envelope{
				ownerRefusalFile(scopeA.ScopeID, gA1.GenerationID, "collide", gA1.ObservedAt, "a1"),
				ownerRefusalFile(scopeA.ScopeID, gA1.GenerationID, "a1-other", gA1.ObservedAt, "a1"),
			}); err != nil {
				t.Fatalf("commit A1: %v", err)
			}
			// The victim scope needs a prior active generation to keep serving.
			priorGen := gA1.GenerationID
			if tc.otherScope {
				gB1 := p.ownerRefusalGeneration(scopeB.ScopeID, "gen-b1")
				if err := p.ownerRefusalCommit(ctx, scopeB, gB1, []facts.Envelope{
					ownerRefusalFile(scopeB.ScopeID, gB1.GenerationID, "b1-file", gB1.ObservedAt, "b1"),
				}); err != nil {
					t.Fatalf("commit B1: %v", err)
				}
				priorGen = gB1.GenerationID
			}
			p.project() // activate the prior generations, as the projector does

			ownerFact := p.queryString(`SELECT row_to_json(f)::text FROM fact_records f WHERE fact_id = 'collide'`)
			ownerKey := p.queryString(`SELECT row_to_json(k)::text FROM relationship_reference_candidate_keys k WHERE fact_id = 'collide'`)
			countRows := func(table string) string {
				return p.queryString(fmt.Sprintf(`SELECT count(*)::text FROM %s`, table))
			}
			factsBefore, workBefore := countRows("fact_records"), countRows("fact_work_items")
			generationsBefore := countRows("scope_generations")
			activeBefore := p.queryString(`SELECT COALESCE(active_generation_id, '') FROM ingestion_scopes WHERE scope_id = $1`, victim.ScopeID)
			if activeBefore != priorGen {
				t.Fatalf("victim scope active generation = %q before the refused commit, want %q", activeBefore, priorGen)
			}

			// The refused generation: one colliding id among fresh ones.
			gBad := p.ownerRefusalGeneration(victim.ScopeID, "gen-bad")
			commitCtx, disposition := collector.WithCommitDisposition(ctx)
			if err := p.ownerRefusalCommit(commitCtx, victim, gBad, []facts.Envelope{
				ownerRefusalFile(victim.ScopeID, gBad.GenerationID, "bad-fresh-1", gBad.ObservedAt, "bad"),
				ownerRefusalFile(victim.ScopeID, gBad.GenerationID, "collide", gBad.ObservedAt, "bad"),
				ownerRefusalFile(victim.ScopeID, gBad.GenerationID, "bad-fresh-2", gBad.ObservedAt, "bad"),
			}); err != nil {
				t.Fatalf("CommitScopeGeneration error = %v, want nil (a refusal, not a failure)", err)
			}

			refusal, refused := disposition.Refusal()
			if !refused || refusal.Reason != telemetry.RefreshSkipReasonFactIDOwnerConflict {
				t.Fatalf("disposition = %v / %+v, want refused with reason fact_id_owner_conflict", disposition.Kind(), refusal)
			}
			// Nothing of the generation landed.
			if got := p.queryString(`SELECT count(*)::text FROM fact_records WHERE generation_id = $1`, gBad.GenerationID); got != "0" {
				t.Fatalf("%s fact rows from the refused generation, want 0", got)
			}
			if got := p.queryString(`SELECT count(*)::text FROM scope_generations WHERE generation_id = $1`, gBad.GenerationID); got != "0" {
				t.Fatalf("refused generation has a scope_generations row")
			}
			if got := p.queryString(`SELECT count(*)::text FROM fact_work_items WHERE generation_id = $1`, gBad.GenerationID); got != "0" {
				t.Fatalf("%s projector work items for the refused generation, want 0", got)
			}
			if got := p.queryString(`SELECT count(*)::text FROM relationship_reference_candidate_keys WHERE generation_id = $1`, gBad.GenerationID); got != "0" {
				t.Fatalf("%s reference-key rows from the refused generation, want 0", got)
			}
			for table, before := range map[string]string{"fact_records": factsBefore, "fact_work_items": workBefore, "scope_generations": generationsBefore} {
				if got := countRows(table); got != before {
					t.Fatalf("%s rows = %s after the refusal, want %s (unchanged)", table, got, before)
				}
			}
			// The owner's rows are byte-identical.
			if got := p.queryString(`SELECT row_to_json(f)::text FROM fact_records f WHERE fact_id = 'collide'`); got != ownerFact {
				t.Fatalf("owner fact row changed:\n before %s\n after  %s", ownerFact, got)
			}
			if got := p.queryString(`SELECT row_to_json(k)::text FROM relationship_reference_candidate_keys k WHERE fact_id = 'collide'`); got != ownerKey {
				t.Fatalf("owner reference-key row changed:\n before %s\n after  %s", ownerKey, got)
			}
			// The victim scope keeps serving its prior generation.
			if got := p.queryString(`SELECT COALESCE(active_generation_id, '') FROM ingestion_scopes WHERE scope_id = $1`, victim.ScopeID); got != priorGen {
				t.Fatalf("victim scope active generation = %q after the refusal, want %q", got, priorGen)
			}

			p.assertOwnerRefusalSignals(&logs, reader, tc.reason, victim.ScopeID, gBad.GenerationID)
		})
	}
}

// assertOwnerRefusalSignals checks the operator signals of one refusal: the
// outcome counter, the conflict counter by reason, the process-local skipped
// refresh count, and exactly one payload-free ERROR log with the ids.
func (p *reprojectProof) assertOwnerRefusalSignals(logs *bytes.Buffer, reader metric.Reader, reason, scopeID, generationID string) {
	p.t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		p.t.Fatalf("Collect() error = %v", err)
	}
	conflicts := map[string]int64{}
	outcomes := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				attr := func(key string) string { v, _ := point.Attributes.Value(attribute.Key(key)); return v.AsString() }
				switch m.Name {
				case "eshu_dp_fact_id_scope_conflicts_total":
					conflicts[attr("reason")] += point.Value
				case "eshu_dp_ingestion_generation_outcomes_total":
					outcomes[attr("outcome")+"/"+attr("reason")] += point.Value
				}
			}
		}
	}
	if len(conflicts) != 1 || conflicts[reason] != 1 {
		p.t.Fatalf("fact id scope conflict counter = %v, want exactly {%s: 1}", conflicts, reason)
	}
	if outcomes["skipped/fact_id_owner_conflict"] != 1 {
		p.t.Fatalf("generation outcomes = %v, want skipped/fact_id_owner_conflict = 1", outcomes)
	}
	if got := telemetry.SkippedRefreshCountByReason(telemetry.RefreshSkipReasonFactIDOwnerConflict); got != 1 {
		p.t.Fatalf("SkippedRefreshCountByReason(fact_id_owner_conflict) = %d, want 1", got)
	}

	var errorLines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		if record["level"] == "ERROR" {
			errorLines = append(errorLines, record)
		}
		if record["msg"] == "fact_id_owner_conflict" && strings.Contains(line, "payload") {
			p.t.Fatalf("the refusal log carries a payload: %s", line)
		}
	}
	if len(errorLines) != 1 || errorLines[0]["msg"] != "fact_id_owner_conflict" {
		p.t.Fatalf("ERROR logs = %v, want exactly one fact_id_owner_conflict", errorLines)
	}
	record := errorLines[0]
	if record["scope_id"] != scopeID || record["generation_id"] != generationID || record["conflict_count"] != float64(1) {
		p.t.Fatalf("ERROR log ids = %v, want scope %q generation %q count 1", record, scopeID, generationID)
	}
	sample, _ := record["conflict"].(map[string]any)
	if sample["fact_id"] != "collide" || sample["fact_kind"] != "file" ||
		sample["incoming_scope_id"] != scopeID || sample["incoming_generation_id"] != generationID ||
		sample["stored_scope_id"] == nil || sample["stored_generation_id"] == nil {
		p.t.Fatalf("ERROR log sample = %v, want fact id, kind, and incoming and stored owners", sample)
	}
}
