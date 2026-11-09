// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	rekeyCommitter = "scope:10408:committer"
	rekeyHolder    = "scope:10408:holder"
	rekeyCVE       = "CVE-2026-0401"
	rekeyMember    = "GHSA-0401-bbbb"
	rekeyPrivate   = "GHSA-0401-aaaa"
	rekeyPackageA  = "pkg:npm/rekey-10408-a@1.0.0"
	rekeyPackageB  = "pkg:npm/rekey-10408-b@1.0.0"
)

// rekeyProof is the live harness for the #10408 re-key fan-out proofs: two
// scopes whose findings share alias-group members (or diverge on private
// member ids), a real fact loader, writer, fencing issuer and queue, and a
// drain loop that plays the reducer worker (claim, Handle, Ack) until the
// queue is empty.
type rekeyProof struct {
	t       *testing.T
	ctx     context.Context
	db      *sql.DB
	queue   postgres.ReducerQueue
	handler supplychaincore.SupplyChainImpactHandler
	fanOut  supplychaincore.SupplyChainImpactFanOut
	handled []string
	// holderBudget, when positive, routes the holder scope's passes through
	// a handler with that evidence budget. holderHandler caches it.
	holderBudget  int
	holderHandler *supplychaincore.SupplyChainImpactHandler
}

func newRekeyProof(t *testing.T) *rekeyProof {
	t.Helper()
	ctx, db := fanOutLiveDB(t)
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	fanOut := fanOutLiveStore(db)
	return &rekeyProof{
		t: t, ctx: ctx, db: db, queue: fanOutLiveQueue(db), fanOut: fanOut,
		handler: supplychaincore.SupplyChainImpactHandler{
			FactLoader:         postgres.NewFactStore(postgres.SQLDB{DB: db}),
			FencingTokenIssuer: postgres.PostgresSupplyChainImpactFencingTokenIssuer{DB: postgres.SQLDB{DB: db}},
			Writer:             supplychaincore.PostgresSupplyChainImpactWriter{DB: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}}, Now: func() time.Time { return now }},
			FanOut:             fanOut,
			Now:                func() time.Time { return now },
		},
	}
}

func (c *rekeyProof) envelope(scope, generation, id, kind string, payload map[string]any) facts.Envelope {
	c.t.Helper()
	observed := time.Date(2026, 10, 31, 11, 0, 0, 0, time.UTC)
	return facts.Envelope{
		FactID: id, ScopeID: scope, GenerationID: fanOutGen(scope, generation), FactKind: kind, StableFactKey: id,
		SchemaVersion: chain.VulnerabilityIntelligenceSchemaVersionV1, CollectorKind: "synthetic",
		SourceConfidence: facts.SourceConfidenceReported, ObservedAt: observed, Payload: payload,
		SourceRef: facts.Ref{SourceSystem: "synthetic", ScopeID: scope, GenerationID: fanOutGen(scope, generation), FactKey: id},
	}
}

func (c *rekeyProof) upsert(envelopes ...facts.Envelope) {
	c.t.Helper()
	if err := postgres.NewFactStore(postgres.SQLDB{DB: c.db}).UpsertFacts(c.ctx, envelopes); err != nil {
		c.t.Fatalf("upsert facts: %v", err)
	}
}

func (c *rekeyProof) consumption(scope, generation, id, pkg, repo string) facts.Envelope {
	return c.envelope(scope, generation, id, factschema.FactKindReducerPackageConsumptionCorrelation, map[string]any{
		"package_id": pkg, "relationship_kind": "consumption", "repository_id": repo,
		"dependency_range": "1.0.0", "canonical_writes": 1, "evidence_fact_ids": []string{id + ":manifest"},
	})
}

func (c *rekeyProof) affectedPackage(scope, generation, id, advisory, pkg string) facts.Envelope {
	return c.envelope(scope, generation, id, chain.VulnerabilityAffectedPackageFactKind, map[string]any{
		"advisory_id": advisory, "package_id": pkg, "ecosystem": "npm",
		"package_name": "rekey-10408", "affected_versions": []string{"1.0.0"}, "fixed_versions": []string{"1.1.0"},
	})
}

func (c *rekeyProof) drain() {
	c.t.Helper()
	for i := 0; i < 60; i++ {
		intent, ok, err := c.queue.Claim(c.ctx)
		if err != nil {
			c.t.Fatalf("claim: %v", err)
		}
		if !ok {
			return
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
	}
	c.t.Fatalf("queue still had work after 60 claims; handled %v", c.handled)
}

func (c *rekeyProof) claim(wantScope string) reducercontract.Intent {
	c.t.Helper()
	intent, ok, err := c.queue.Claim(c.ctx)
	if err != nil || !ok {
		c.t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if wantScope != "" && intent.ScopeID != wantScope {
		c.t.Fatalf("claimed %s, want %s", intent.ScopeID, wantScope)
	}
	return intent
}

func (c *rekeyProof) rerun(scope string) {
	c.t.Helper()
	if _, err := c.db.ExecContext(c.ctx, `
UPDATE fact_work_items SET status='pending', attempt_count=0, lease_owner=NULL, claim_until=NULL,
  visible_at=now(), next_attempt_at=NULL, payload = payload - 'fanout_origin'
WHERE scope_id=$1 AND domain='supply_chain_impact' AND status='succeeded'`, scope); err != nil {
		c.t.Fatalf("rerun %s: %v", scope, err)
	}
}

// findingKeys returns every scope's active finding rows as scope/cve/advisory
// triples for diagnosis and convergence assertions.
func (c *rekeyProof) findingKeys() []string {
	c.t.Helper()
	rows, err := c.db.QueryContext(c.ctx, `
SELECT f.scope_id, COALESCE(f.payload->>'cve_id',''), COALESCE(f.payload->>'advisory_id',''),
       COALESCE(f.payload #>> '{provenance,advisory_group_members}', '')
FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
ORDER BY 1, 2, 3`)
	if err != nil {
		c.t.Fatalf("read finding keys: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var scope, cve, advisory, members string
		if err := rows.Scan(&scope, &cve, &advisory, &members); err != nil {
			c.t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%s cve=%s advisory=%s members=%s", scope, cve, advisory, members))
	}
	if err := rows.Err(); err != nil {
		c.t.Fatalf("iterate finding keys: %v", err)
	}
	return out
}

// keysByScope returns the sorted cve_id keys of one scope's active findings.
func (c *rekeyProof) keysByScope(scope string) []string {
	c.t.Helper()
	rows, err := c.db.QueryContext(c.ctx, `
SELECT DISTINCT COALESCE(f.payload->>'cve_id','') FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = 'reducer_supply_chain_impact_finding' AND NOT f.is_tombstone
  AND f.scope_id = $1 ORDER BY 1`, scope)
	if err != nil {
		c.t.Fatalf("read scope keys: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			c.t.Fatal(err)
		}
		out = append(out, key)
	}
	if err := rows.Err(); err != nil {
		c.t.Fatalf("iterate scope keys: %v", err)
	}
	sort.Strings(out)
	return out
}

func (c *rekeyProof) handledCount(scope string) int {
	n := 0
	for _, s := range c.handled {
		if s == scope {
			n++
		}
	}
	return n
}

// copyGeneration copies one scope's non-finding facts to a new active
// generation, the way a collector rescan would, and seeds its queue item.
func (c *rekeyProof) copyGeneration(scope, from, to string) {
	c.t.Helper()
	old := fanOutGen(scope, from)
	if _, err := c.db.ExecContext(c.ctx, `UPDATE scope_generations SET status='superseded', superseded_at=now() WHERE generation_id = $1`, old); err != nil {
		c.t.Fatalf("supersede: %v", err)
	}
	gen := fanOutGen(scope, to)
	if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', now(), now(), 'active', now(), '{}'::jsonb)`, gen, scope); err != nil {
		c.t.Fatalf("insert generation: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scope, gen); err != nil {
		c.t.Fatalf("activate: %v", err)
	}
	if _, err := c.db.ExecContext(c.ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT fact_id || ':' || $2::text, scope_id, $3, fact_kind, stable_fact_key, schema_version, collector_kind,
  source_confidence, source_system, source_fact_key, observed_at, ingested_at, false, payload
FROM fact_records WHERE scope_id = $1 AND generation_id = $4 AND fact_kind <> 'reducer_supply_chain_impact_finding'`,
		scope, to, gen, old); err != nil {
		c.t.Fatalf("copy facts: %v", err)
	}
	fanOutSeedItemAt(c.t, c.ctx, c.db, scope, gen)
}

// seedLateAliasPair seeds the criterion-9 late-alias world: the committer
// holds a lone CVE finding, and the holder holds a GHSA-only advisory group
// (no CVE anywhere) keyed by a private advisory id the committer never sees.
// The packages differ per scope so neither scope's closure absorbs the other.
func (c *rekeyProof) seedLateAliasPair() {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyCommitter, "g1")
	fanOutSeedScope(c.t, c.ctx, c.db, rekeyHolder, "g1")
	c.upsert(
		c.envelope(rekeyCommitter, "g1", "fact:10408:cve", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": rekeyCVE, "cve_id": rekeyCVE, "source": "nvd", "cvss_score": 7.5, "aliases": []string{rekeyCVE},
		}),
		c.affectedPackage(rekeyCommitter, "g1", "fact:10408:pkg", rekeyCVE, rekeyPackageA),
		c.consumption(rekeyCommitter, "g1", "fact:10408:cons", rekeyPackageA, "repository:r_10408_c"),
		c.envelope(rekeyHolder, "g1", "fact:10408:haaa", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": rekeyPrivate, "source": "osv", "cvss_score": 4.0, "aliases": []string{rekeyMember},
		}),
		c.envelope(rekeyHolder, "g1", "fact:10408:hbbb", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": rekeyMember, "source": "osv", "cvss_score": 9.0, "aliases": []string{rekeyMember},
		}),
		c.affectedPackage(rekeyHolder, "g1", "fact:10408:hpkg", rekeyPrivate, rekeyPackageB),
		c.consumption(rekeyHolder, "g1", "fact:10408:hcons", rekeyPackageB, "repository:r_10408_h"),
	)
	fanOutSeedItem(c.t, c.ctx, c.db, rekeyCommitter, "g1")
	fanOutSeedItem(c.t, c.ctx, c.db, rekeyHolder, "g1")
}

// addLateAliasRecord models the late alias arrival (#10408 ruling 1a): a
// GHSA-only record aliasing the CVE arrives in the committer's new
// generation. The committer merges; the holder must follow through fan-out.
func (c *rekeyProof) addLateAliasRecord() {
	c.t.Helper()
	c.copyGeneration(rekeyCommitter, "g1", "g2")
	c.upsert(c.envelope(rekeyCommitter, "g2", "fact:10408:osv:g2", chain.VulnerabilityCVEFactKind, map[string]any{
		"advisory_id": rekeyMember, "source": "osv", "cvss_score": 5.0, "aliases": []string{rekeyCVE},
	}))
}

// TestRekeyLateAliasReopensAdvisoryKeyedHolderLive is the criterion-9 proof:
// a GHSA-only record aliasing the CVE arrives in the committer's scope. The
// committer merges to the CVE group; the holder's advisory-keyed row (its
// group id is a private advisory id no cve_id arm names, its deciding record
// names the merged member) must reopen with no manual rerun.
func TestRekeyLateAliasReopensAdvisoryKeyedHolderLive(t *testing.T) {
	c := newRekeyProof(t)
	c.seedLateAliasPair()
	c.drain()
	t.Logf("pre-merge findings: %v", c.findingKeys())
	if got := c.keysByScope(rekeyHolder); len(got) != 1 || got[0] != rekeyPrivate {
		t.Fatalf("holder keys = %v, want the lone GHSA-only group [%s]", got, rekeyPrivate)
	}

	c.addLateAliasRecord()
	c.rerun(rekeyCommitter)
	c.handled = nil
	c.drain()
	t.Logf("post-merge findings: %v", c.findingKeys())
	t.Logf("post-merge handled: %v", c.handled)
	if c.handledCount(rekeyHolder) == 0 {
		t.Fatalf("holder never reopened after the late alias (handled %v; findings %v)", c.handled, c.findingKeys())
	}
	if got := c.keysByScope(rekeyHolder); len(got) != 1 || got[0] != rekeyCVE {
		t.Fatalf("holder keys = %v, want the merged CVE group [%s]", got, rekeyCVE)
	}
	if got := c.keysByScope(rekeyCommitter); len(got) != 1 || got[0] != rekeyCVE {
		t.Fatalf("committer keys = %v, want the merged CVE group [%s]", got, rekeyCVE)
	}
}
