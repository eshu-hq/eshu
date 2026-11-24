// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit_test

import (
	"context"
	"strings"
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

// supportsReady answers the supports readiness predicate as ready, so a
// supports-served call reaches its read instead of refusing at the probe.
func supportsReady(recorder *audit.Recorder) {
	recorder.Rows = func(sql string) db.Rows {
		if strings.Contains(sql, "supply_chain_impact_finding_supports_backfill") {
			return audit.StaticRows([]any{true})
		}
		return nil
	}
}

// explainReady answers the supports readiness predicate and a fresh winners
// watermark, so the supplement route of the count, inventory and grouped reads
// reaches its read statement.
func explainReady(recorder *audit.Recorder) {
	recorder.Rows = func(sql string) db.Rows {
		switch {
		case strings.Contains(sql, "supply_chain_impact_finding_supports_backfill"):
			return audit.StaticRows([]any{true})
		case sql == impact.SelectWinnersWatermarkQuery:
			return audit.StaticRows([]any{time.Now().UTC()})
		}
		return nil
	}
}

func listProbe(variant string, gate bool, mutate func(*impact.FindingFilter)) probe {
	return probe{"supply_chain.impact_findings.list", variant, func(ctx context.Context, recorder *audit.Recorder) {
		supportsReady(recorder)
		store := impact.NewPostgresFindingStoreWithReadStore(recorder, gate)
		_, _ = store.ListSupplyChainImpactFindings(ctx, impactFilter(mutate))
		// The list handler reads the winners watermark for the truth envelope's
		// freshness (applyWinnersFreshness); the store probes it only with the
		// gate on.
		_, _ = store.SupplyChainImpactWinnersWatermark(ctx)
	}}
}

// readinessRows answers the readiness gate and the image-reference digest
// lookup so a target-resolution anchor reaches the readiness statements.
func readinessRows(sql string) db.Rows {
	switch {
	case strings.Contains(sql, "to_regclass('eshu_security.role_policy_state')"):
		// Production runs the protected readiness statement; the fallback statement
		// is only for isolated schemas that lack the authority layout.
		return audit.StaticRows([]any{true})
	case strings.Contains(sql, "package_manifest_consumption_key_backfill_markers"):
		return audit.StaticRows([]any{true})
	case strings.Contains(sql, "container_image_identity_current_supports") && strings.Contains(sql, "digest"):
		return audit.StaticRows([]any{"sha256:abc"})
	case strings.Contains(sql, "package_registry.package") && strings.Contains(sql, "SELECT DISTINCT"):
		return audit.StaticRows([]any{"pkg:npm/x@1", "npm", "x", "x", ""})
	case strings.Contains(sql, "vulnerability.affected_package") && strings.Contains(sql, "SELECT DISTINCT"):
		// One resolved package, so the target-key ownership check runs.
		return audit.StaticRows([]any{"pkg:npm/x@1", "pkg:npm/x@1", "npm", "x"})
	default:
		return nil
	}
}

// readinessProbes drives the readiness store the list and explain handlers call
// for the readiness block of their responses, for a repository anchor and for
// the three target-resolution anchors (CVE, image reference, package).
func readinessProbes(capability string) []probe {
	run := func(query impact.ReadinessQuery, protected bool) func(context.Context, *audit.Recorder) {
		return func(ctx context.Context, recorder *audit.Recorder) {
			recorder.Rows = func(sql string) db.Rows {
				if !protected && strings.Contains(sql, "to_regclass('eshu_security.role_policy_state')") {
					return nil // an empty cursor reads as "layout absent": the fallback statement
				}
				return readinessRows(sql)
			}
			_, _ = impact.NewPostgresReadinessStoreWithReadStore(recorder).ReadSupplyChainImpactReadiness(ctx, query)
		}
	}
	var out []probe
	for _, protected := range []bool{false, true} {
		layout := "fallback statement"
		if protected {
			layout = "protected statement"
		}
		for name, query := range map[string]impact.ReadinessQuery{
			"repository anchor": {RepositoryID: "repository:r"},
			// The CVE and package anchors resolve their target first; the canned
			// rows stop that resolution before the readiness statement (the
			// package identity check rejects them), so these two variants cover
			// target resolution only. The repository and image-reference anchors
			// reach the readiness statements.
			"cve anchor (target resolution only)":     {CVEID: "CVE-1"},
			"image reference anchor":                  {ImageRef: "registry.example/app:1"},
			"package anchor (target resolution only)": {PackageID: "pkg:npm/x@1"},
		} {
			out = append(out, probe{capability, "readiness, " + layout + ", " + name, run(query, protected)})
		}
	}
	return out
}

func aggregateProbe(variant string, gate bool, mutate func(*impact.AggregateFilter)) probe {
	return probe{"supply_chain.impact_findings.aggregate", variant, func(ctx context.Context, recorder *audit.Recorder) {
		explainReady(recorder) // supports readiness and a fresh winners watermark, so the supplement route reaches its statement
		store := impact.NewPostgresAggregateStoreWithReadStore(recorder, gate)
		_, _ = store.CountSupplyChainImpactFindings(ctx, aggregateFilter(mutate))
		_, _ = store.SupplyChainImpactInventory(ctx, aggregateFilter(mutate), impact.InventoryByStatus, 10, 0)
	}}
}

// probes lists every capability this audit drives. A capability absent here
// must appear in testdata/probe-waivers.txt.
func probes() []probe {
	out := append(readinessProbes("supply_chain.impact_findings.list"), readinessProbes("supply_chain.impact_explanation.read")...)
	return append(out, coreProbes()...)
}

func coreProbes() []probe {
	return []probe{
		listProbe("gate off", false, nil),
		listProbe("gate on, ungranted", true, nil),
		listProbe("gate on, scope grant", true, granted),
		listProbe("gate on, scope grant, image_ref", true, func(f *impact.FindingFilter) { granted(f); imageRef(f) }),
		listProbe("gate on, scope grant, precise profile", true, func(f *impact.FindingFilter) { granted(f); precise(f) }),
		listProbe("gate on, image_ref", true, imageRef),
		listProbe("gate on, precise profile", true, precise),
		aggregateProbe("gate off", false, nil),
		aggregateProbe("gate on, ungranted", true, nil),
		aggregateProbe("gate on, repository grant", true, grantedAgg),
		aggregateProbe("gate on, repository grant, image_ref", true, func(f *impact.AggregateFilter) { grantedAgg(f); imageRefAgg(f) }),
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
		{"supply_chain.impact_findings.aggregate", "grouped inventory, precise profile", func(ctx context.Context, recorder *audit.Recorder) {
			explainReady(recorder)
			_, _ = impact.NewPostgresGroupedInventoryStoreWithReadStore(recorder, true, nil).
				SupplyChainImpactGroupedInventory(ctx, aggregateFilter(preciseAgg), impact.GroupedInventoryByService, 10, 0)
		}},
		{"supply_chain.impact_findings.aggregate", "grouped inventory, image_ref", func(ctx context.Context, recorder *audit.Recorder) {
			explainReady(recorder)
			_, _ = impact.NewPostgresGroupedInventoryStoreWithReadStore(recorder, true, nil).
				SupplyChainImpactGroupedInventory(ctx, aggregateFilter(imageRefAgg), impact.GroupedInventoryByService, 10, 0)
		}},
		{"supply_chain.impact_findings.aggregate", "grouped inventory, granted", func(ctx context.Context, recorder *audit.Recorder) {
			supportsReady(recorder)
			_, _ = impact.NewPostgresGroupedInventoryStoreWithReadStore(recorder, true, nil).
				SupplyChainImpactGroupedInventory(ctx, aggregateFilter(func(f *impact.AggregateFilter) { grantedAgg(f); imageRefAgg(f) }),
					impact.GroupedInventoryByService, 10, 0)
		}},
		{"supply_chain.impact_explanation.read", "granted selector, gate on", func(ctx context.Context, recorder *audit.Recorder) {
			supportsReady(recorder)
			_, _ = impact.NewPostgresFindingStoreWithReadStore(recorder, true).
				ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{CVEID: "CVE-1", RepositoryID: "repository:r", AllowedScopeIDs: []string{"scope:a"}})
		}},
		{"supply_chain.impact_explanation.read", "ungranted image selector, gate on", func(ctx context.Context, recorder *audit.Recorder) {
			supportsReady(recorder)
			_, _ = impact.NewPostgresFindingStoreWithReadStore(recorder, true).
				ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{CVEID: "CVE-1", ImageRef: "registry.example/a:1"})
		}},
		{"supply_chain.impact_explanation.read", "by public finding id, gate on", func(ctx context.Context, recorder *audit.Recorder) {
			supportsReady(recorder)
			_, _ = impact.NewPostgresFindingStoreWithReadStore(recorder, true).
				ExplainSupplyChainImpact(ctx, packet.ExplanationFilter{FindingID: "finding:x"})
		}},
		{"supply_chain.advisory_evidence.list", "repository anchored, gate on", func(ctx context.Context, recorder *audit.Recorder) {
			supportsReady(recorder)
			_, _ = advisory.NewPostgresEvidenceStoreWithReadModel(recorder, true).
				ListAdvisoryEvidence(ctx, advisory.EvidenceFilter{RepositoryID: "repository:r", Limit: 10})
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
		{"supply_chain.security_alert_reconciliations.aggregate", "count and inventory", func(ctx context.Context, recorder *audit.Recorder) {
			store := alerts.NewPostgresAggregateStoreWithReadStore(recorder)
			filter := supplychain.SecurityAlertReconciliationAggregateFilter{RepositoryID: "repository:r"}
			_, _ = store.CountSecurityAlertReconciliations(ctx, filter)
			_, _ = store.SecurityAlertReconciliationInventory(ctx, filter, supplychain.SecurityAlertReconciliationInventoryByStatus, 10, 0)
		}},
		{"supply_chain.security_alert_reconciliations.list", "repository anchored", func(ctx context.Context, recorder *audit.Recorder) {
			_, _ = alerts.NewPostgresStoreWithReadStore(recorder).
				ListSecurityAlertReconciliations(ctx, supplychain.SecurityAlertReconciliationFilter{RepositoryID: "repository:r", Limit: 10})
		}},
	}
}
