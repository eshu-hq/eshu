// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	multiVersionLiveCVE      = "CVE-2026-11113"
	multiVersionLivePackage  = "npm://registry.npmjs.org/brace-expansion"
	multiVersionLiveRepo     = "repository:r_11113"
	multiVersionLiveAffected = "1.1.11"
	multiVersionLiveFixed    = "5.0.13"
)

// newMultiVersionConvergence is the #9695 convergence harness (real loader,
// writer, fencing issuer, fan-out store and queue) with no seeded scopes.
func newMultiVersionConvergence(t *testing.T) *convergence {
	t.Helper()
	ctx, db := fanOutLiveDB(t)
	c := &convergence{t: t, ctx: ctx, db: db, queue: fanOutLiveQueue(db)}
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
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
	return c
}

// seedMultiVersionHolder writes one holder scope with the advisory and two
// consumption rows of one repository: an affected and a fixed version of one
// package. fixedFirst decides which row the loader returns first (it reads in
// observed_at order), so two holders can see the rows in opposite orders.
func (c *convergence) seedMultiVersionHolder(scope string, fixedFirst bool) {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, scope, "g1")
	gen := fanOutGen(scope, "g1")
	row := func(id, version string, offset time.Duration) facts.Envelope {
		envelope := c.envelope(scope, gen, id, factschema.FactKindReducerPackageConsumptionCorrelation, map[string]any{
			"package_id": multiVersionLivePackage, "relationship_kind": "consumption", "repository_id": multiVersionLiveRepo,
			"dependency_range": version, "installed_version": version, "lockfile": true,
			"canonical_writes": 1, "evidence_fact_ids": []string{id + ":manifest"},
		})
		envelope.ObservedAt = envelope.ObservedAt.Add(offset)
		return envelope
	}
	affectedOffset, fixedOffset := time.Minute, time.Duration(0)
	if !fixedFirst {
		affectedOffset, fixedOffset = 0, time.Minute
	}
	affected := row("fact:11113:"+scope+":affected", multiVersionLiveAffected, affectedOffset)
	fixed := row("fact:11113:"+scope+":fixed", multiVersionLiveFixed, fixedOffset)
	c.upsert(
		c.envelope(scope, gen, "fact:11113:"+scope+":cve", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": multiVersionLiveCVE, "cve_id": multiVersionLiveCVE, "source": "osv", "cvss_score": 7.5,
			"aliases": []any{multiVersionLiveCVE},
		}),
		c.envelope(scope, gen, "fact:11113:"+scope+":affected_package", chain.VulnerabilityAffectedPackageFactKind, map[string]any{
			"advisory_id": multiVersionLiveCVE, "cve_id": multiVersionLiveCVE, "package_id": multiVersionLivePackage,
			"ecosystem": "npm", "package_name": "brace-expansion", "fixed_versions": []any{"2.0.2"},
			"affected_ranges": []any{map[string]any{"type": "SEMVER", "events": []any{
				map[string]any{"introduced": "0"}, map[string]any{"fixed": "2.0.2"},
			}}},
		}),
	)
	c.upsert(affected, fixed)
}

// multiVersionWinner is one canonical winners row for the advisory.
type multiVersionWinner struct {
	repositoryID, version, status string
	sourceCount                   int
}

func (c *convergence) multiVersionWinners() []multiVersionWinner {
	c.t.Helper()
	rows, err := c.db.QueryContext(c.ctx, `
SELECT repository_id, observed_version, impact_status, source_count
FROM supply_chain_impact_canonical_winners WHERE cve_id = $1
ORDER BY repository_id, observed_version`, multiVersionLiveCVE)
	if err != nil {
		c.t.Fatalf("read winners: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []multiVersionWinner
	for rows.Next() {
		var w multiVersionWinner
		if err := rows.Scan(&w.repositoryID, &w.version, &w.status, &w.sourceCount); err != nil {
			c.t.Fatalf("scan winner: %v", err)
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		c.t.Fatalf("winners rows: %v", err)
	}
	return out
}

// TestSupplyChainImpactWinnersResweepMultiVersionRepositoryLive pins issue #11113 over the real
// handler, writer, queue and keyed winners resweep: two holder scopes each
// hold one repository installing an affected and a fixed version of one
// package, the loader returns the rows in opposite orders in the two scopes,
// and the scopes run in both arrival orders. The winners must hold exactly
// one key per (repository, version), each copied by both holders, whatever
// the order. Before the fix each holder stored one finding judged on its
// first row, so the holders disagreed and the affected version could vanish.
func TestSupplyChainImpactWinnersResweepMultiVersionRepositoryLive(t *testing.T) {
	want := []multiVersionWinner{
		{repositoryID: multiVersionLiveRepo, version: multiVersionLiveAffected, status: "affected_exact", sourceCount: 2},
		{repositoryID: multiVersionLiveRepo, version: multiVersionLiveFixed, status: "not_affected_known_fixed", sourceCount: 2},
	}
	for _, order := range [][2]string{{"a", "b"}, {"b", "a"}} {
		t.Run(fmt.Sprintf("%s_then_%s", order[0], order[1]), func(t *testing.T) {
			c := newMultiVersionConvergence(t)
			winners := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: c.db})
			if _, err := winners.ResweepWinners(c.ctx); err != nil {
				t.Fatalf("startup winners resweep: %v", err)
			}
			scopes := map[string]bool{"a": true, "b": false} // fixed row first in a, affected row first in b
			for _, name := range order {
				scope := "scope:11113:holder-" + name
				c.seedMultiVersionHolder(scope, scopes[name])
				fanOutSeedItem(c.t, c.ctx, c.db, scope, "g1")
				c.drain()
				if _, err := winners.ResweepWinnersKeyed(c.ctx); err != nil {
					t.Fatalf("keyed winners tick after %s: %v", scope, err)
				}
			}
			if got := c.multiVersionWinners(); !slices.Equal(got, want) {
				t.Fatalf("winners = %+v, want %+v (handled %v)", got, want, c.handled)
			}
		})
	}
}
