// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit_test

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/query/store/audit"
	supplychain "github.com/eshu-hq/eshu/go/internal/query/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/advisory"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/alerts"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// A probe drives one production store through one branch that can choose a
// different answering store, against a Recorder, and returns what it issued.
// The variants a capability lists are the gate and grant branches; every
// variant that reads a different store must be named here, because the audit
// only sees what a probe drives (the package census covers the rest).
type probe struct {
	capability string
	variant    string
	run        func(ctx context.Context, recorder *audit.Recorder)
}

func impactFilter(mutate func(*impact.FindingFilter)) impact.FindingFilter {
	filter := impact.FindingFilter{Limit: 10, Status: "affected_exact"}
	if mutate != nil {
		mutate(&filter)
	}
	return filter
}

func aggregateFilter(mutate func(*impact.AggregateFilter)) impact.AggregateFilter {
	filter := impact.AggregateFilter{Status: "affected_exact"}
	if mutate != nil {
		mutate(&filter)
	}
	return filter
}

func precise(filter *impact.FindingFilter)  { filter.DetectionProfile = impact.ProfilePrecise }
func granted(filter *impact.FindingFilter)  { filter.AllowedScopeIDs = []string{"scope:a"} }
func imageRef(filter *impact.FindingFilter) { filter.ImageRef = "registry.example/app:1" }
func grantedAgg(filter *impact.AggregateFilter) {
	filter.AllowedRepositoryIDs = []string{"repository:r"}
}
func imageRefAgg(filter *impact.AggregateFilter) { filter.ImageRef = "registry.example/app:1" }
func preciseAgg(filter *impact.AggregateFilter)  { filter.DetectionProfile = impact.ProfilePrecise }

func listProbe(variant string, gate bool, mutate func(*impact.FindingFilter)) probe {
	return probe{"supply_chain.impact_findings.list", variant, func(ctx context.Context, recorder *audit.Recorder) {
		_, _ = impact.NewPostgresFindingStoreWithReadStore(recorder, gate).
			ListSupplyChainImpactFindings(ctx, impactFilter(mutate))
	}}
}

func aggregateProbe(variant string, gate bool, mutate func(*impact.AggregateFilter)) probe {
	return probe{"supply_chain.impact_findings.aggregate", variant, func(ctx context.Context, recorder *audit.Recorder) {
		store := impact.NewPostgresAggregateStoreWithReadStore(recorder, gate)
		_, _ = store.CountSupplyChainImpactFindings(ctx, aggregateFilter(mutate))
		_, _ = store.SupplyChainImpactInventory(ctx, aggregateFilter(mutate), impact.InventoryByStatus, 10, 0)
	}}
}

// probes lists every capability this audit drives. A capability absent here
// must appear in testdata/probe-waivers.txt.
func probes() []probe {
	return []probe{
		listProbe("gate off", false, nil),
		listProbe("gate on, ungranted", true, nil),
		listProbe("gate on, scope grant", true, granted),
		listProbe("gate on, image_ref", true, imageRef),
		listProbe("gate on, precise profile", true, precise),
		aggregateProbe("gate off", false, nil),
		aggregateProbe("gate on, ungranted", true, nil),
		aggregateProbe("gate on, repository grant", true, grantedAgg),
		aggregateProbe("gate on, image_ref", true, imageRefAgg),
		aggregateProbe("gate on, precise profile", true, preciseAgg),
		{"supply_chain.impact_findings.aggregate", "grouped inventory", func(ctx context.Context, recorder *audit.Recorder) {
			// A fresh watermark lets the store reach its grouped statement.
			recorder.Rows = func(sql string) db.Rows {
				if sql == impact.SelectWinnersWatermarkQuery {
					return audit.StaticRows([]any{time.Now()})
				}
				return nil
			}
			_, _ = impact.NewPostgresGroupedInventoryStoreWithReadStore(recorder, true, nil).
				SupplyChainImpactGroupedInventory(ctx, aggregateFilter(nil), impact.GroupedInventoryByService, 10, 0)
		}},
		{"supply_chain.impact_explanation.read", "by public finding id", func(ctx context.Context, recorder *audit.Recorder) {
			_, _ = impact.NewPostgresFindingStoreWithReadStore(recorder, false).
				ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{FindingID: "finding:x"})
		}},
		{"supply_chain.impact_explanation.read", "by selector", func(ctx context.Context, recorder *audit.Recorder) {
			_, _ = impact.NewPostgresFindingStoreWithReadStore(recorder, true).
				ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{CVEID: "CVE-1", RepositoryID: "repository:r"})
		}},
		{"supply_chain.advisory_evidence.list", "repository anchored", func(ctx context.Context, recorder *audit.Recorder) {
			_, _ = advisory.NewPostgresEvidenceStoreWithReadStore(recorder).
				ListAdvisoryEvidence(ctx, advisory.EvidenceFilter{RepositoryID: "repository:r", Limit: 10})
		}},
		{"supply_chain.advisory_evidence.list", "advisory anchored", func(ctx context.Context, recorder *audit.Recorder) {
			_, _ = advisory.NewPostgresEvidenceStoreWithReadStore(recorder).
				ListAdvisoryEvidence(ctx, advisory.EvidenceFilter{CVEID: "CVE-1", Limit: 10})
		}},
		{"supply_chain.advisory_catalog.list", "page", func(ctx context.Context, recorder *audit.Recorder) {
			_, _ = advisory.NewPostgresCatalogStoreWithReadStore(recorder).
				ListAdvisoryCatalog(ctx, advisory.CatalogFilter{Limit: 10})
		}},
		{"supply_chain.security_alert_reconciliations.list", "repository anchored", func(ctx context.Context, recorder *audit.Recorder) {
			_, _ = alerts.NewPostgresStoreWithReadStore(recorder).
				ListSecurityAlertReconciliations(ctx, supplychain.SecurityAlertReconciliationFilter{RepositoryID: "repository:r", Limit: 10})
		}},
	}
}
