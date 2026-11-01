// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	convergenceCVE      = "CVE-2026-9695"
	convergencePackage  = "pkg:npm/fanout-9695@1.0.0"
	convergenceRepo     = "repository:r_9695"
	convergenceSource   = "scope:9695:nvd"
	convergenceHolder   = "scope:9695:epss"
	convergenceOperator = "operator:vulnerability_suppressions"
)

// convergence is the harness for the end-to-end fan-out proofs: three scopes that
// each hold a copy of one CVE's finding (the NVD source scope, an EPSS scope
// whose own facts never change, and the operator suppression scope), a real
// fact loader, writer, fencing issuer and queue, and a drain loop that plays the
// reducer worker (claim, Handle, Ack) until the queue is empty.
type convergence struct {
	t       *testing.T
	ctx     context.Context
	db      *sql.DB
	queue   postgres.ReducerQueue
	handler supplychaincore.SupplyChainImpactHandler
	fanOut  supplychaincore.SupplyChainImpactFanOut
	// handled records the scope of every Handle call, in order.
	handled []string
}

func newConvergence(t *testing.T, withOperator bool) *convergence {
	t.Helper()
	ctx, db := fanOutLiveDB(t)
	c := &convergence{t: t, ctx: ctx, db: db, queue: fanOutLiveQueue(db)}
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	c.fanOut = fanOutLiveStore(db)
	c.handler = supplychaincore.SupplyChainImpactHandler{
		FactLoader:         postgres.NewFactStore(postgres.SQLDB{DB: db}),
		FencingTokenIssuer: postgres.PostgresSupplyChainImpactFencingTokenIssuer{DB: postgres.SQLDB{DB: db}},
		Writer: supplychaincore.PostgresSupplyChainImpactWriter{
			DB:  postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
			Now: func() time.Time { return now },
		},
		FanOut: c.fanOut,
		Now:    func() time.Time { return now },
	}
	c.seedSource()
	c.seedHolder()
	if withOperator {
		c.seedOperator()
	}
	return c
}

func (c *convergence) envelope(scope, generation, id, kind string, payload map[string]any) facts.Envelope {
	observed := time.Date(2026, 10, 31, 11, 0, 0, 0, time.UTC)
	return facts.Envelope{
		FactID: id, ScopeID: scope, GenerationID: generation, FactKind: kind, StableFactKey: id,
		SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1, CollectorKind: "synthetic",
		SourceConfidence: facts.SourceConfidenceReported, ObservedAt: observed, Payload: payload,
		SourceRef: facts.Ref{SourceSystem: "synthetic", ScopeID: scope, GenerationID: generation, FactKey: id},
	}
}

func (c *convergence) upsert(envelopes ...facts.Envelope) {
	c.t.Helper()
	if err := postgres.NewFactStore(postgres.SQLDB{DB: c.db}).UpsertFacts(c.ctx, envelopes); err != nil {
		c.t.Fatalf("upsert facts: %v", err)
	}
}

func (c *convergence) seedSource() {
	fanOutSeedScope(c.t, c.ctx, c.db, convergenceSource, "g1")
	gen := fanOutGen(convergenceSource, "g1")
	c.upsert(
		c.envelope(convergenceSource, gen, "fact:9695:cve", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": convergenceCVE, "cve_id": convergenceCVE, "source": "nvd", "cvss_score": 7.5, "status": "Analyzed",
		}),
		c.envelope(convergenceSource, gen, "fact:9695:affected", chain.VulnerabilityAffectedPackageFactKind, map[string]any{
			"advisory_id": convergenceCVE, "cve_id": convergenceCVE, "package_id": convergencePackage, "ecosystem": "npm",
			"package_name": "fanout-9695", "affected_versions": []string{"1.0.0"}, "fixed_versions": []string{"1.1.0"},
		}),
		c.envelope(convergenceSource, gen, "fact:9695:consumption", factschema.FactKindReducerPackageConsumptionCorrelation, map[string]any{
			"package_id": convergencePackage, "relationship_kind": "consumption", "repository_id": convergenceRepo,
			"dependency_range": "1.0.0", "canonical_writes": 1, "evidence_fact_ids": []string{"fact:9695:manifest"},
		}),
	)
	fanOutSeedItem(c.t, c.ctx, c.db, convergenceSource, "g1")
}

func (c *convergence) seedHolder() {
	fanOutSeedScope(c.t, c.ctx, c.db, convergenceHolder, "g1")
	gen := fanOutGen(convergenceHolder, "g1")
	c.upsert(c.envelope(convergenceHolder, gen, "fact:9695:epss", chain.VulnerabilityEPSSScoreFactKind, map[string]any{
		"cve_id": convergenceCVE, "probability": "0.5", "percentile": "0.9", "score_date": "2026-10-31",
	}))
	fanOutSeedItem(c.t, c.ctx, c.db, convergenceHolder, "g1")
}

// seedOperator creates an accepted_risk suppression for the advisory through the
// real mutation endpoint, which writes the operator scope's generation.
func (c *convergence) seedOperator() {
	handler := &query.SupplyChainHandler{SuppressionMutations: query.NewPostgresVulnerabilitySuppressionMutationStore(c.db)}
	mux := http.NewServeMux()
	handler.Mount(mux)
	body := `{"suppression_id":"suppression-9695","justification":"accepted_risk","authored_at":"2026-10-31T11:30:00Z",
		"reason":"compensating control","evidence_ref":"https://registry.example.com/evidence/9695","scope":{"advisory_id":"` + convergenceCVE + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/supply-chain/impact/suppressions", strings.NewReader(body))
	req = req.WithContext(query.ContextWithAuthContext(req.Context(), query.AuthContext{
		Mode: query.AuthModeShared, SubjectClass: "shared_token", SubjectIDHash: "sha256:synthetic-operator", AllScopes: true,
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		c.t.Fatalf("create suppression status = %d body = %s", rec.Code, rec.Body.String())
	}
	var response query.VulnerabilitySuppressionMutationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		c.t.Fatalf("decode suppression response: %v", err)
	}
	activateAdvisorySuppressionGeneration(c.t, c.ctx, c.db, response.GenerationID)
	fanOutSeedItemAt(c.t, c.ctx, c.db, convergenceOperator, response.GenerationID)
}

// drain plays the reducer worker until the queue has nothing claimable. A
// handler error is recorded as a failed attempt, like the worker does.
func (c *convergence) drain() {
	c.t.Helper()
	for i := 0; i < 60; i++ {
		intent, ok, err := c.queue.Claim(c.ctx)
		if err != nil {
			c.t.Fatalf("claim: %v", err)
		}
		if !ok {
			return
		}
		c.handled = append(c.handled, fmt.Sprintf("%s#%d", intent.ScopeID, intent.AttemptCount))
		result, err := c.handler.Handle(c.ctx, intent)
		if err != nil {
			if failErr := c.queue.Fail(c.ctx, intent, err); failErr != nil {
				c.t.Fatalf("fail %s: %v (handle error: %v)", intent.IntentID, failErr, err)
			}
			// Retry delay is a second; make the failed item claimable now.
			if _, execErr := c.db.ExecContext(c.ctx, `UPDATE fact_work_items SET visible_at = now() - interval '1 second', next_attempt_at = NULL WHERE work_item_id = $1 AND status = 'retrying'`, intent.IntentID); execErr != nil {
				c.t.Fatalf("make retry visible: %v", execErr)
			}
			continue
		}
		if err := c.queue.Ack(c.ctx, intent, result); err != nil {
			c.t.Fatalf("ack %s: %v", intent.IntentID, err)
		}
	}
	c.t.Fatalf("queue still had work after 60 claims; handled %v", c.handled)
}

// rerun makes a scope's current item claimable again, the way a new generation or
// an operator replay would.
func (c *convergence) rerun(scope string) {
	c.t.Helper()
	if _, err := c.db.ExecContext(c.ctx, `
UPDATE fact_work_items SET status='pending', attempt_count=0, lease_owner=NULL, claim_until=NULL,
  visible_at=now(), next_attempt_at=NULL, payload = payload - 'fanout_origin'
WHERE scope_id=$1 AND domain='supply_chain_impact' AND status='succeeded'`, scope); err != nil {
		c.t.Fatalf("rerun %s: %v", scope, err)
	}
}

// rescanSource plays a collector rescan of the source scope: a new generation
// holding the same facts under new fact ids (a fact id embeds its generation), the
// CVE record's status optionally changed, activated, with a pending item. The old
// generation stays behind as superseded, like production until retention prunes it.
func (c *convergence) rescanSource(generation, status string) {
	c.t.Helper()
	gen := fanOutGen(convergenceSource, generation)
	old := fanOutGen(convergenceSource, "g1")
	if _, err := c.db.ExecContext(c.ctx, `UPDATE scope_generations SET status='superseded', superseded_at=now() WHERE generation_id = $1`, old); err != nil {
		c.t.Fatalf("supersede: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', now(), now(), 'active', now(), '{}'::jsonb)`, gen, convergenceSource); err != nil {
		c.t.Fatalf("insert generation: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, convergenceSource, gen); err != nil {
		c.t.Fatalf("activate: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT fact_id || ':' || $2::text, scope_id, $3, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, false,
  CASE WHEN fact_id = 'fact:9695:cve' AND $4::text <> '' THEN jsonb_set(payload, '{status}', to_jsonb($4::text)) ELSE payload END
FROM fact_records WHERE scope_id = $1 AND generation_id = $5 AND fact_kind <> 'reducer_supply_chain_impact_finding'`,
		convergenceSource, generation, gen, status, old); err != nil {
		c.t.Fatalf("copy facts: %v", err)
	}
	fanOutSeedItemAt(c.t, c.ctx, c.db, convergenceSource, gen)
}

func (c *convergence) setSourceStatus(status string) {
	c.t.Helper()
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_records SET payload = jsonb_set(payload, '{status}', to_jsonb($1::text)) WHERE fact_id = 'fact:9695:cve'`, status); err != nil {
		c.t.Fatalf("set status: %v", err)
	}
}

// copies returns each scope's active finding state for the CVE.
func (c *convergence) copies() map[string]string {
	c.t.Helper()
	rows, err := c.db.QueryContext(c.ctx, `
SELECT f.scope_id, COALESCE(f.payload->>'suppression_state','') FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = $1 AND NOT f.is_tombstone AND f.payload->>'cve_id' = $2`, facts.ReducerSupplyChainImpactFindingFactKind, convergenceCVE)
	if err != nil {
		c.t.Fatalf("read copies: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var scope, state string
		if err := rows.Scan(&scope, &state); err != nil {
			c.t.Fatal(err)
		}
		out[scope] = state
	}
	return out
}

// winnerState rebuilds the canonical winners and returns the surfaced state of
// the CVE's finding.
func (c *convergence) winnerState() string {
	c.t.Helper()
	if err := (postgres.SupplyChainImpactWinnersStore{DB: postgres.SQLDB{DB: c.db}}).RebuildAllWinners(c.ctx, time.Now().UTC()); err != nil {
		c.t.Fatalf("rebuild winners: %v", err)
	}
	store := query.NewPostgresSupplyChainImpactFindingStoreWithReadModel(c.db, true)
	rows, err := store.ListSupplyChainImpactFindings(c.ctx, query.SupplyChainImpactFindingFilter{CVEID: convergenceCVE, IncludeSuppressed: true, Limit: 10})
	if err != nil {
		c.t.Fatalf("list findings: %v", err)
	}
	if len(rows) != 1 {
		c.t.Fatalf("winners = %d rows, want exactly one canonical finding", len(rows))
	}
	if rows[0].Suppression == nil {
		return "active"
	}
	return rows[0].Suppression.State
}

func (c *convergence) wantCopies(want map[string]string) {
	c.t.Helper()
	got := c.copies()
	if len(got) != len(want) {
		c.t.Fatalf("copies = %v, want %v (handled %v)", got, want, c.handled)
	}
	for scope, state := range want {
		if got[scope] != state {
			c.t.Fatalf("copies = %v, want %v (handled %v)", got, want, c.handled)
		}
	}
}

// TestRejectedAdvisoryReopensOperatorScopeAndRetractsItsAcceptedRiskCopyLive is O1.
// The operator scope holds an accepted_risk copy written while the CVE was live.
// The source rejects the CVE; nothing about the operator scope's own facts
// changes, so only the fan-out reaches it. After the drain every copy, the
// operator's included, is advisory_retracted and the canonical finding says so.
func TestRejectedAdvisoryReopensOperatorScopeAndRetractsItsAcceptedRiskCopyLive(t *testing.T) {
	c := newConvergence(t, true)
	c.drain()
	c.wantCopies(map[string]string{convergenceSource: "accepted_risk", convergenceHolder: "accepted_risk", convergenceOperator: "accepted_risk"})
	if got := c.winnerState(); got != "accepted_risk" {
		t.Fatalf("live winner = %q, want the operator's accepted_risk", got)
	}

	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)
	c.drain()
	c.wantCopies(map[string]string{
		convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted", convergenceOperator: "advisory_retracted",
	})
	if got := c.winnerState(); got != "advisory_retracted" {
		t.Fatalf("winner after the rejection = %q, want advisory_retracted", got)
	}
}

// TestRestoredAdvisoryReopensOperatorScopeAndRestoresItsAcceptedRiskDecisionLive is
// O2: the record is restored, the fan-out re-derives the retracted operator copy,
// and the operator's standing decision applies again instead of the copy staying
// hidden as advisory_retracted.
func TestRestoredAdvisoryReopensOperatorScopeAndRestoresItsAcceptedRiskDecisionLive(t *testing.T) {
	c := newConvergence(t, true)
	c.drain()
	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)
	c.drain()
	// The restore only has something to undo once the rejection reached every copy.
	c.wantCopies(map[string]string{
		convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted", convergenceOperator: "advisory_retracted",
	})

	c.setSourceStatus("Analyzed")
	c.rerun(convergenceSource)
	c.drain()
	c.wantCopies(map[string]string{convergenceSource: "accepted_risk", convergenceHolder: "accepted_risk", convergenceOperator: "accepted_risk"})
	if got := c.winnerState(); got != "accepted_risk" {
		t.Fatalf("winner after the restore = %q, want the operator's accepted_risk", got)
	}
}

// TestRestoredAdvisoryFansOutToStaleRetractedHolderCopiesLive is V2 at the winners
// layer: the EPSS scope's copy was retracted and its own facts never change, so
// before the fan-out a restored record left that stale copy winning. Now the
// restore reopens it and the canonical finding is active again.
func TestRestoredAdvisoryFansOutToStaleRetractedHolderCopiesLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)
	c.drain()
	c.wantCopies(map[string]string{convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted"})

	c.setSourceStatus("Analyzed")
	c.rerun(convergenceSource)
	c.drain()
	c.wantCopies(map[string]string{convergenceSource: "active", convergenceHolder: "active"})
	if got := c.winnerState(); got != "active" {
		t.Fatalf("winner after the restore = %q, want active", got)
	}
}

// flakyFindFanOut fails the first holder read, like a statement timeout.
type flakyFindFanOut struct {
	supplychaincore.SupplyChainImpactFanOut
	failed atomic.Bool
}

func (f *flakyFindFanOut) FindHolderScopes(ctx context.Context, own string, cves []string) ([]string, error) {
	if f.failed.CompareAndSwap(false, true) {
		return nil, supplychaincore.ErrFanOutReadTimeout
	}
	return f.SupplyChainImpactFanOut.FindHolderScopes(ctx, own, cves)
}

// TestFailedHolderReadRetriesTheWholePassAndStillConvergesLive is the at-least-once
// proof on the real queue: the first holder read fails after the writer
// committed, the item is retried, the rewrite changes nothing (the rows are
// already identical), and the retry still fans out because its attempt count
// shows an earlier attempt ran.
func TestFailedHolderReadRetriesTheWholePassAndStillConvergesLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	flaky := &flakyFindFanOut{SupplyChainImpactFanOut: c.fanOut}
	c.handler.FanOut = flaky
	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)
	c.drain()
	if !flaky.failed.Load() {
		t.Fatal("the injected holder read failure never fired")
	}
	c.wantCopies(map[string]string{convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted"})
}

// TestFanOutReopenedPassesDoNotFanOutAgainLive is the one-hop proof on the real
// queue: every holder pass the fan-out reopens runs once and the drain ends, so a
// reopened pass that rewrote its own copy did not reopen anyone back.
func TestFanOutReopenedPassesDoNotFanOutAgainLive(t *testing.T) {
	c := newConvergence(t, true)
	c.drain()
	c.handled = nil
	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)
	c.drain()
	counts := map[string]int{}
	for _, entry := range c.handled {
		scope, _, _ := strings.Cut(entry, "#")
		counts[scope]++
	}
	for _, scope := range []string{convergenceSource, convergenceHolder, convergenceOperator} {
		if counts[scope] != 1 {
			t.Fatalf("handled counts = %v, want each scope's pass exactly once", counts)
		}
	}
}

// TestRescanWithIdenticalContentFansOutNothingLive is the storm guard on the real
// queue. A finding's fact id embeds its generation, so before the previous
// generation became the baseline, the first pass of every rescan looked fully
// changed and reopened every holder of every CVE. A rescan that derived the same
// findings must run its own pass and nobody else's.
func TestRescanWithIdenticalContentFansOutNothingLive(t *testing.T) {
	c := newConvergence(t, true)
	c.drain()
	c.handled = nil
	c.rescanSource("g2", "")
	c.drain()
	if len(c.handled) != 1 || !strings.HasPrefix(c.handled[0], convergenceSource+"#") {
		t.Fatalf("handled = %v, want only the rescanned source scope: an unchanged rescan reopened holders", c.handled)
	}
	c.wantCopies(map[string]string{convergenceSource: "accepted_risk", convergenceHolder: "accepted_risk", convergenceOperator: "accepted_risk"})
}

// TestRescanThatRejectsTheRecordFansOutLive is the other half on the real queue:
// the rescan's record is rejected, so the first pass of the new generation reports
// the change against the previous generation and the holders converge.
func TestRescanThatRejectsTheRecordFansOutLive(t *testing.T) {
	c := newConvergence(t, true)
	c.drain()
	c.handled = nil
	c.rescanSource("g2", "Rejected")
	c.drain()
	c.wantCopies(map[string]string{
		convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted", convergenceOperator: "advisory_retracted",
	})
}
