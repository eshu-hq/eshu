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
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// seedCarryMergePair seeds the suppression-carry merge world (#10408,
// criterion 5a): like seedMergePair, but the dead record is withdrawn (not
// re-aliased) on rescan. The committer holds a survivor and a dead CVE
// record; the holder holds the dead id as a singleton. The operator
// suppression names the dead (higher) id.
func (c *rekeyProof) seedCarryMergePair() {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyCommitter, "g1")
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyHolder, "g1")
	c.upsert(
		c.envelope(rekeyCommitter, "g1", "fact:10408:cve", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": rekeySurvivor, "cve_id": rekeySurvivor, "source": "ghsa", "cvss_score": 7.5, "aliases": []string{rekeySurvivor},
		}),
		c.envelope(rekeyCommitter, "g1", "fact:10408:dead", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": rekeyDead, "cve_id": rekeyDead, "source": "nvd", "cvss_score": 5.0, "aliases": []string{rekeyDead},
		}),
		c.affectedPackage(rekeyCommitter, "g1", "fact:10408:pkg", rekeySurvivor, rekeyPackageA),
		c.affectedPackage(rekeyCommitter, "g1", "fact:10408:dpkg", rekeyDead, rekeyPackageB),
		c.consumption(rekeyCommitter, "g1", "fact:10408:cons", rekeyPackageA, "repository:r_10408_c"),
		c.consumption(rekeyCommitter, "g1", "fact:10408:dcons", rekeyPackageB, "repository:r_10408_c"),
		c.envelope(rekeyHolder, "g1", "fact:10408:hdead", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": rekeyDead, "cve_id": rekeyDead, "source": "nvd", "cvss_score": 5.0, "aliases": []string{rekeyDead},
		}),
		c.affectedPackage(rekeyHolder, "g1", "fact:10408:hdpkg", rekeyDead, rekeyPackageB),
		c.consumption(rekeyHolder, "g1", "fact:10408:hdcons", rekeyPackageB, "repository:r_10408_h"),
	)
	fanOutSeedItem(c.t, c.ctx, c.db, rekeyCommitter, "g1")
	fanOutSeedItem(c.t, c.ctx, c.db, rekeyHolder, "g1")
}

// rescanWithdrawDeadRecord copies the committer to a second generation
// without the dead CVE record and without its affected-package row, and
// points the survivor's aliases at the dead id: the merge edge survives on
// the survivor side while nothing but aliases still names the dead id,
// modelling a source withdrawal with a retained alias. The dead id must not
// reach the pass filter through any CVE/advisory arm — only the L3 alias
// arm may pull the dead-id suppression.
func (c *rekeyProof) rescanWithdrawDeadRecord() {
	c.t.Helper()
	c.copyGeneration(rekeyCommitter, "g1", "g2")
	if _, err := c.db.ExecContext(c.ctx, `
DELETE FROM fact_records WHERE scope_id = $1 AND generation_id = $2 AND fact_id IN ('fact:10408:dead:g2', 'fact:10408:dpkg:g2')`, rekeyCommitter, fanOutGen(rekeyCommitter, "g2")); err != nil {
		c.t.Fatalf("withdraw dead record: %v", err)
	}
	aliases, err := json.Marshal([]string{rekeySurvivor, rekeyDead})
	if err != nil {
		c.t.Fatalf("marshal aliases: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `
UPDATE fact_records SET payload = jsonb_set(payload, '{aliases}', $3::jsonb)
WHERE scope_id = $1 AND generation_id = $2 AND fact_id = 'fact:10408:cve:g2'`, rekeyCommitter, fanOutGen(rekeyCommitter, "g2"), string(aliases)); err != nil {
		c.t.Fatalf("point survivor at dead id: %v", err)
	}
}

// rescanWithdrawHolderDeadRecord copies the holder to a second generation
// without its dead singleton record and affected-package row, mirroring the
// committer withdrawal. After this point no scope holds a record naming the
// dead id: only aliases still reference it, so only the L3 alias arm can
// pull the dead-id suppression.
func (c *rekeyProof) rescanWithdrawHolderDeadRecord() {
	c.t.Helper()
	c.copyGeneration(rekeyHolder, "g1", "g2")
	if _, err := c.db.ExecContext(c.ctx, `
DELETE FROM fact_records WHERE scope_id = $1 AND generation_id = $2 AND fact_id IN ('fact:10408:hdead:g2', 'fact:10408:hdpkg:g2')`, rekeyHolder, fanOutGen(rekeyHolder, "g2")); err != nil {
		c.t.Fatalf("withdraw holder dead record: %v", err)
	}
}

// seedCarrySuppression creates an operator suppression with the given id,
// scope anchor JSON, and timestamps through the real mutation endpoint. An
// empty expiresAt means timeless. It does not activate or seed the operator
// item: successive mutations accumulate into newer generations, so the caller
// finalizes once with activateCarryOperator.
func (c *rekeyProof) seedCarrySuppression(id, scopeJSON, authoredAt, expiresAt, justification string) {
	c.t.Helper()
	handler := &query.SupplyChainHandler{SuppressionMutations: query.NewPostgresVulnerabilitySuppressionMutationStore(c.db)}
	mux := http.NewServeMux()
	handler.Mount(mux)
	expiry := ""
	if expiresAt != "" {
		expiry = `,"expires_at":"` + expiresAt + `"`
	}
	body := `{"suppression_id":"` + id + `","justification":"` + justification + `","authored_at":"` + authoredAt + `"` +
		expiry + `,"reason":"carry-10408","scope":{` + scopeJSON + `}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/supply-chain/impact/suppressions", strings.NewReader(body))
	req = req.WithContext(query.ContextWithAuthContext(req.Context(), query.AuthContext{
		Mode: query.AuthModeShared, SubjectClass: "shared_token", SubjectIDHash: "sha256:synthetic-operator", AllScopes: true,
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		c.t.Fatalf("create suppression %s status = %d body = %s", id, rec.Code, rec.Body.String())
	}
}

// activateCarryOperator activates the latest operator generation (superseding
// any earlier mutation generation, which only the newest accumulated set may
// be active) and seeds its reducer item.
func (c *rekeyProof) activateCarryOperator() {
	c.t.Helper()
	var latest string
	if err := c.db.QueryRowContext(c.ctx, `
SELECT generation_id FROM scope_generations
WHERE scope_id = 'operator:vulnerability_suppressions'
ORDER BY ingested_at DESC, generation_id DESC LIMIT 1`).Scan(&latest); err != nil {
		c.t.Fatalf("read latest operator generation: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `
UPDATE scope_generations SET status = 'superseded', superseded_at = now()
WHERE scope_id = 'operator:vulnerability_suppressions' AND generation_id <> $1 AND status = 'active'`, latest); err != nil {
		c.t.Fatalf("supersede earlier operator generations: %v", err)
	}
	activateAdvisorySuppressionGeneration(c.t, c.ctx, c.db, latest)
	fanOutSeedItemAt(c.t, c.ctx, c.db, "operator:vulnerability_suppressions", latest)
}

// suppressionIDs returns each active finding key of one scope mapped to the
// persisted suppressing suppression id (empty when none matched).
func (c *rekeyProof) suppressionIDs(scope string) map[string]string {
	c.t.Helper()
	rows, err := c.db.QueryContext(c.ctx, `
SELECT COALESCE(f.payload->>'cve_id',''), COALESCE(f.payload #>> '{suppression,suppression_id}','')
FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
  AND f.scope_id = $1`, scope)
	if err != nil {
		c.t.Fatalf("read suppression ids: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var key, id string
		if err := rows.Scan(&key, &id); err != nil {
			c.t.Fatal(err)
		}
		out[key] = id
	}
	if err := rows.Err(); err != nil {
		c.t.Fatalf("iterate suppression ids: %v", err)
	}
	return out
}

// countFindingRows returns the number of active finding rows of one scope,
// for the no-duplicate assertions.
func (c *rekeyProof) countFindingRows(scope string) int {
	c.t.Helper()
	var n int
	if err := c.db.QueryRowContext(c.ctx, `
SELECT COUNT(*) FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
  AND f.scope_id = $1`, scope).Scan(&n); err != nil {
		c.t.Fatalf("count finding rows: %v", err)
	}
	return n
}

// pendingWorkItems returns the number of the scope's reducer items stuck in
// an unhealthy state (failed, retrying, or dead-lettered), for the
// no-dead-letter assertions. Succeeded and superseded items are healthy
// terminals: a replayed or superseded generation is done, not stranded.
func (c *rekeyProof) pendingWorkItems(scope string) int {
	c.t.Helper()
	var n int
	if err := c.db.QueryRowContext(c.ctx, `
SELECT COUNT(*) FROM fact_work_items WHERE scope_id = $1 AND domain = 'supply_chain_impact'
  AND status IN ('failed', 'retrying', 'dead_letter')`, scope).Scan(&n); err != nil {
		c.t.Fatalf("count pending work items: %v", err)
	}
	return n
}

// drainCapturing drains exactly like drain but keeps each pass result keyed
// by scope and generation (last pass wins), for sub-signal assertions. The
// claim/handle/ack path is identical; only the results are retained.
func (c *rekeyProof) drainCapturing() map[string]reducercontract.Result {
	c.t.Helper()
	out := map[string]reducercontract.Result{}
	for i := 0; i < 60; i++ {
		intent, ok, err := c.queue.Claim(c.ctx)
		if err != nil {
			c.t.Fatalf("claim: %v", err)
		}
		if !ok {
			return out
		}
		c.handled = append(c.handled, intent.ScopeID)
		result, err := c.handlerFor(intent.ScopeID).Handle(c.ctx, intent)
		if err != nil {
			if failErr := c.queue.Fail(c.ctx, intent, err); failErr != nil {
				c.t.Fatalf("fail %s: %v (handle error: %v)", intent.IntentID, failErr, err)
			}
			if _, execErr := c.db.ExecContext(c.ctx, `UPDATE fact_work_items SET visible_at = now() - interval '1 second', next_attempt_at = NULL WHERE work_item_id = $1 AND status = 'retrying'`, intent.IntentID); execErr != nil {
				c.t.Fatalf("make retry visible: %v", execErr)
			}
			continue
		}
		if err := c.queue.Ack(c.ctx, intent, result); err != nil {
			c.t.Fatalf("ack %s: %v", intent.IntentID, err)
		}
		out[intent.ScopeID+"@"+intent.GenerationID] = result
	}
	c.t.Fatalf("queue still had work after 60 claims; handled %v", c.handled)
	return nil
}

// suppressionPayloads returns the byte-ordered payloads of the operator
// scope's active suppression facts, for the byte-unchanged assertion.
func (c *rekeyProof) suppressionPayloads() []string {
	c.t.Helper()
	rows, err := c.db.QueryContext(c.ctx, `
SELECT payload::text FROM fact_records
WHERE scope_id = 'operator:vulnerability_suppressions' AND fact_kind = 'vulnerability.suppression' AND NOT is_tombstone
ORDER BY fact_id`)
	if err != nil {
		c.t.Fatalf("read suppression payloads: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			c.t.Fatal(err)
		}
		out = append(out, payload)
	}
	if err := rows.Err(); err != nil {
		c.t.Fatalf("iterate suppression payloads: %v", err)
	}
	return out
}

// suppressionStates returns each active finding key of one scope mapped to
// its persisted suppression_state, for the carry assertions.
func (c *rekeyProof) suppressionStates(scope string) map[string]string {
	c.t.Helper()
	rows, err := c.db.QueryContext(c.ctx, `
SELECT COALESCE(f.payload->>'cve_id',''), COALESCE(f.payload->>'suppression_state','')
FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
  AND f.scope_id = $1`, scope)
	if err != nil {
		c.t.Fatalf("read suppression states: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var key, state string
		if err := rows.Scan(&key, &state); err != nil {
			c.t.Fatal(err)
		}
		out[key] = state
	}
	if err := rows.Err(); err != nil {
		c.t.Fatalf("iterate suppression states: %v", err)
	}
	return out
}

// TestRekeyCarryDeadIDSuppressionLive is the criterion-5a proof: the operator
// suppression names the dead (higher) id. After the committer withdraws the
// dead record with the alias retained on the survivor, the merged finding
// must stay hidden by member-set match, on the committer and on the fanned
// out holder, with the suppression fact itself byte-unchanged (carry by
// re-evaluation, never by rewriting the suppression).
func TestRekeyCarryDeadIDSuppressionLive(t *testing.T) {
	c := newRekeyProof(t)
	c.seedCarryMergePair()
	c.seedRekeyOperator(rekeyDead)
	c.drain()
	t.Logf("pre-merge findings: %v", c.findingKeys())
	before := c.suppressionPayloads()
	if len(before) == 0 {
		t.Fatalf("no suppression facts seeded in the operator scope")
	}
	if got := c.suppressionStates(rekeyCommitter)[rekeyDead]; got == "" || got == "active" {
		t.Fatalf("pre-merge dead finding state = %q, want it suppressed by the dead-id suppression", got)
	}

	c.rescanWithdrawDeadRecord()
	c.rerun(rekeyCommitter)
	c.handled = nil
	c.drain()
	t.Logf("post-merge findings: %v", c.findingKeys())
	t.Logf("post-merge handled: %v", c.handled)
	if got := c.keysByScope(rekeyCommitter); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("committer keys = %v, want the survivor group [%s]", got, rekeySurvivor)
	}
	if got := c.suppressionStates(rekeyCommitter)[rekeySurvivor]; got == "" || got == "active" || got == "expired" {
		t.Fatalf("merged committer finding state = %q, want it hidden by the carried dead-id suppression", got)
	}
	if c.handledCount(rekeyHolder) == 0 {
		t.Fatalf("holder never reopened after the merge (handled %v)", c.handled)
	}
	if got := c.keysByScope(rekeyHolder); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("holder keys = %v, want the survivor group [%s]", got, rekeySurvivor)
	}
	if got := c.suppressionStates(rekeyHolder)[rekeySurvivor]; got == "" || got == "active" || got == "expired" {
		t.Fatalf("merged holder finding state = %q, want it hidden by the carried dead-id suppression", got)
	}
	// Drive the holder to withdrawal as well: no scope holds a record
	// naming the dead id anymore, only the survivor's aliases reference it.
	// Both scopes must re-derive suppressed through the alias arm alone.
	c.rescanWithdrawHolderDeadRecord()
	c.rerun(rekeyHolder)
	c.rerun(rekeyCommitter)
	c.handled = nil
	c.drain()
	t.Logf("post-withdrawal findings: %v", c.findingKeys())
	if got := c.keysByScope(rekeyCommitter); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("committer keys = %v, want the survivor group [%s]", got, rekeySurvivor)
	}
	if got := c.suppressionStates(rekeyCommitter)[rekeySurvivor]; got == "" || got == "active" || got == "expired" {
		t.Fatalf("withdrawn committer finding state = %q, want it hidden by the carried dead-id suppression", got)
	}
	if got := c.keysByScope(rekeyHolder); len(got) != 1 || got[0] != rekeySurvivor {
		t.Fatalf("holder keys = %v, want the survivor group [%s]", got, rekeySurvivor)
	}
	if got := c.suppressionStates(rekeyHolder)[rekeySurvivor]; got == "" || got == "active" || got == "expired" {
		t.Fatalf("withdrawn holder finding state = %q, want it hidden by the carried dead-id suppression", got)
	}
	after := c.suppressionPayloads()
	if len(after) != len(before) {
		t.Fatalf("suppression facts = %d after the merge, want %d (carry must not rewrite suppressions)", len(after), len(before))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("suppression fact %d changed bytes across the merge:\nbefore: %s\nafter:  %s", i, before[i], after[i])
		}
	}
}
