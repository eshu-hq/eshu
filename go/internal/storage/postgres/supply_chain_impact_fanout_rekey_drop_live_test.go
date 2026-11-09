// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// seedDropPair seeds the member-drop world: the committer holds a merged
// {CVE, member} group, and the holder holds the member as a fallback
// singleton. The holder's pass runs under a tiny evidence budget, so its
// grouping stays disabled and the member keeps its per-record key with
// MissingEvidence even though the alias-closed load sees the committer's
// edge — exactly like a 3.2 truncation fallback.
func (c *rekeyProof) seedDropPair(holderBudget int) {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyCommitter, "g1")
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyHolder, "g1")
	c.upsert(
		c.envelope(rekeyCommitter, "g1", "fact:10408:cve", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": rekeyCVE, "cve_id": rekeyCVE, "source": "ghsa", "cvss_score": 7.5, "aliases": []string{rekeyCVE},
		}),
		c.envelope(rekeyCommitter, "g1", "fact:10408:osv", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": rekeyMember, "source": "osv", "cvss_score": 5.0, "aliases": []string{rekeyCVE},
		}),
		c.affectedPackage(rekeyCommitter, "g1", "fact:10408:pkg", rekeyCVE, rekeyPackageA),
		c.consumption(rekeyCommitter, "g1", "fact:10408:cons", rekeyPackageA, "repository:r_10408_c"),
		c.envelope(rekeyHolder, "g1", "fact:10408:hm", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": rekeyMember, "source": "osv", "cvss_score": 5.0, "aliases": []string{rekeyMember},
		}),
		c.affectedPackage(rekeyHolder, "g1", "fact:10408:hpkg", rekeyMember, rekeyPackageB),
		c.consumption(rekeyHolder, "g1", "fact:10408:hcons", rekeyPackageB, "repository:r_10408_h"),
	)
	fanOutSeedItem(c.t, c.ctx, c.db, rekeyCommitter, "g1")
	fanOutSeedItem(c.t, c.ctx, c.db, rekeyHolder, "g1")
	if holderBudget > 0 {
		c.holderBudget = holderBudget
	}
}

// holderBudget, when positive, routes the holder scope's passes through a
// handler with that evidence budget, so the holder truncates while the
// committer loads fully.
func (c *rekeyProof) handlerFor(scope string) supplychaincore.SupplyChainImpactHandler {
	if c.holderBudget > 0 && scope == rekeyHolder {
		if c.holderHandler == nil {
			holder := c.handler
			holder.EvidenceBudget = c.holderBudget
			c.holderHandler = &holder
		}
		return *c.holderHandler
	}
	return c.handler
}

// rescanDropMember copies the committer to a second generation without the
// member record, modelling a member-only removal whose group key survives.
func (c *rekeyProof) rescanDropMember() {
	c.t.Helper()
	old := fanOutGen(rekeyCommitter, "g1")
	if _, err := c.db.ExecContext(c.ctx, `UPDATE scope_generations SET status='superseded', superseded_at=now() WHERE generation_id = $1`, old); err != nil {
		c.t.Fatalf("supersede: %v", err)
	}
	gen := fanOutGen(rekeyCommitter, "g2")
	if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', now(), now(), 'active', now(), '{}'::jsonb)`, gen, rekeyCommitter); err != nil {
		c.t.Fatalf("insert generation: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, rekeyCommitter, gen); err != nil {
		c.t.Fatalf("activate: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT fact_id || ':g2', scope_id, $2, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, false, payload
FROM fact_records WHERE scope_id = $1 AND generation_id = $3 AND fact_kind <> 'reducer_supply_chain_impact_finding'
  AND fact_id <> 'fact:10408:osv'`, rekeyCommitter, gen, old); err != nil {
		c.t.Fatalf("copy facts: %v", err)
	}
	fanOutSeedItemAt(c.t, c.ctx, c.db, rekeyCommitter, gen)
}

// TestRekeyDropReopensFallbackSingletonHolderLive is the touched-set proof:
// the committer's group sheds a member under a surviving group key, so no new
// row names the dropped id. The holder's fallback singleton (per-record key,
// no member set) must reopen with no manual rerun. Before L2 the fanned-out
// changed set names only the survivor and the holder is missed.
func TestRekeyDropReopensFallbackSingletonHolderLive(t *testing.T) {
	c := newRekeyProof(t)
	c.seedDropPair(1)
	c.drain()
	t.Logf("pre-drop findings: %v", c.findingKeys())
	if got := c.keysByScope(rekeyHolder); len(got) != 1 || got[0] != rekeyMember {
		t.Fatalf("holder keys = %v, want the lone fallback singleton [%s]", got, rekeyMember)
	}

	c.rescanDropMember()
	c.rerun(rekeyCommitter)
	c.handled = nil
	c.drain()
	t.Logf("post-drop findings: %v", c.findingKeys())
	t.Logf("post-drop handled: %v", c.handled)
	if c.handledCount(rekeyHolder) == 0 {
		t.Fatalf("holder never reopened after the member drop (handled %v; findings %v)", c.handled, c.findingKeys())
	}
	if got := c.keysByScope(rekeyHolder); len(got) != 1 || got[0] != rekeyMember {
		t.Fatalf("holder keys = %v, want the fallback singleton [%s] intact", got, rekeyMember)
	}
	if got := c.keysByScope(rekeyCommitter); len(got) != 1 || got[0] != rekeyCVE {
		t.Fatalf("committer keys = %v, want the surviving group [%s]", got, rekeyCVE)
	}
}

// seedRekeyOperator creates an accepted_risk suppression scoped to the given
// advisory through the real mutation endpoint and seeds the operator item.
func (c *rekeyProof) seedRekeyOperator(advisory string) {
	c.t.Helper()
	handler := &query.SupplyChainHandler{SuppressionMutations: query.NewPostgresVulnerabilitySuppressionMutationStore(c.db)}
	mux := http.NewServeMux()
	handler.Mount(mux)
	body := `{"suppression_id":"suppression-10408","justification":"accepted_risk","authored_at":"2026-10-31T11:30:00Z",
		"reason":"compensating control","evidence_ref":"https://registry.example.com/evidence/10408","scope":{"advisory_id":"` + advisory + `"}}`
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
	fanOutSeedItemAt(c.t, c.ctx, c.db, "operator:vulnerability_suppressions", response.GenerationID)
}

// TestRekeyDropReopensOperatorScopeLive pins the operator suppression scope as
// a holder like any other: the committer drops the suppressed member, the
// operator copy must reopen with no manual rerun and re-derive under the
// surviving key. The operator mirrors the committer's grouping, so the
// changed set already reaches it; this test guards that path.
func TestRekeyDropReopensOperatorScopeLive(t *testing.T) {
	c := newRekeyProof(t)
	c.seedDropPair(1)
	c.seedRekeyOperator(rekeyMember)
	c.drain()
	t.Logf("pre-drop findings: %v", c.findingKeys())

	c.rescanDropMember()
	c.rerun(rekeyCommitter)
	c.handled = nil
	c.drain()
	t.Logf("post-drop findings: %v", c.findingKeys())
	t.Logf("post-drop handled: %v", c.handled)
	if c.handledCount("operator:vulnerability_suppressions") == 0 {
		t.Fatalf("operator scope never reopened after the member drop (handled %v)", c.handled)
	}
	if got, want := c.keysByScope("operator:vulnerability_suppressions"), c.keysByScope(rekeyCommitter); len(got) == 0 || len(want) == 0 || got[0] != want[0] {
		t.Fatalf("operator keys = %v, committer keys = %v, want the operator copy under the surviving key", got, want)
	}
}
