// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/evidencebundle"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// getLiveEvidenceBundle composes and returns a live evidence_bundle.v1
// artifact (#4045) from the same internal status providers backing GET
// /api/v0/status/index, GET /api/v0/status/pipeline, and GET
// /api/v0/status/collectors, so `eshu evidence bundle export --live` and
// this route describe one running stack through the same status.Report and
// repository-count query rather than two independently maintained readings.
// It never re-derives evidencebundle's redaction or composition rules: it
// only maps typed status.Report data into an evidencebundle.LiveSnapshot and
// hands it to BuildLiveBundle -> Validate -> StampValidation, exactly what
// the CLI's --live path does after decoding the same three routes over HTTP.
//
// The bundle is stack-wide, mirroring the CLI's --live path
// (cmd/eshu/evidence.go, which refuses --scope with --live for the
// same reason): none of the composed status data carries a repository or
// tenant selector. This route is deliberately absent from
// scopedHTTPRouteSupportsTenantFilter (auth_scoped_routes.go) and carries no
// "x-scoped-token-support" OpenAPI marker, the same posture already held by
// its two stack-wide source routes, GET /api/v0/status/index and GET
// /api/v0/status/pipeline (openapi_paths_status.go). AuthMiddleware always
// rejects a scoped-bearer-token caller before this handler runs. A
// browser-session caller's admission is policy-dependent, not a flat reject:
// browserSessionRouteDenialReason (auth_browser_session_route_policy.go)
// admits a tenant-bound all-scopes browser session -- the normal
// single-tenant/local owner console session -- whenever
// BrowserSessionRoutePolicy.AllowTenantBoundAllScopes is set, which
// ScopedRoutePolicyForGovernanceMode does for the default,
// "local_no_policy", and "hosted_single_tenant" governance modes, and which
// cmd/api reads through it (browser_sessions.go). Only an unrecognized or
// hosted-multi-tenant governance mode, or a restricted-scope browser session,
// gets rejected. Either way, the caller who reaches this handler could
// already read the three source status routes directly and derive this
// bundle by hand.
func (h *EvidenceHandler) getLiveEvidenceBundle(w http.ResponseWriter, r *http.Request) {
	if h.StatusReader == nil {
		WriteError(w, http.StatusServiceUnavailable, "status reader not configured")
		return
	}

	// Full selection: listCollectors' collector readiness classification
	// (status.CollectorRuntimeStatuses) folds in CollectorFactEvidence, which
	// the index route's lighter selection (IncludeCollectorFactEvidence:
	// false) deliberately skips (status.go getIndexStatus). The bundle needs
	// both the pipeline and the collector sections, so it always loads the
	// full snapshot rather than the index route's trimmed one.
	_, report, err := loadStatusReport(r.Context(), h.StatusReader, time.Now(), status.DefaultOptions())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
		return
	}

	snapshot := liveEvidenceSnapshotFromReport(report, repositoryCountForEvidenceBundle(r, h.Neo4j))
	bundle := evidencebundle.BuildLiveBundle(snapshot, evidencebundle.LiveBundleOptions{
		CreatedAt: time.Now(),
	})
	if err := evidencebundle.Validate(bundle); err != nil {
		// This is a composition bug on our own inputs, never on caller input,
		// so it is a 500, not a 400.
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("compose live evidence bundle: %v", err))
		return
	}
	bundle = evidencebundle.StampValidation(bundle)
	WriteJSON(w, http.StatusOK, bundle)
}

// repositoryCountForEvidenceBundle mirrors getIndexStatus's repository count
// query (status.go): the same "MATCH (r:Repository) RETURN count(r)" read,
// so both surfaces report the identical count for the identical graph state.
// A nil or failing Neo4j reads as 0, the same ambiguous-zero BuildLiveBundle
// already records as a "repository_count" MissingEvidence entry -- this
// function does not invent a second error path for that ambiguity.
func repositoryCountForEvidenceBundle(r *http.Request, neo4j GraphQuery) int {
	if neo4j == nil {
		return 0
	}
	row, err := neo4j.RunSingle(r.Context(), "MATCH (r:Repository) RETURN count(r) as count", nil)
	if err != nil || row == nil {
		return 0
	}
	return IntVal(row, "count")
}

// liveEvidenceSnapshotFromReport maps a status.Report -- the same typed
// report getPipelineStatus and listCollectors already build via
// loadStatusReport/status.LoadReport -- into an evidencebundle.LiveSnapshot.
// It reuses domainBacklogBuckets and queueBlockageCountsByDomain
// (aws_materialization_status.go) for per-domain blockage, and
// status.CollectorRuntimeStatuses (the same call listCollectors makes) for
// collector readiness, so this mapping never re-derives status truth the
// existing status routes already compute. It also carries
// report.DomainBacklogsTruncated through unchanged: status.BuildReport is the
// single place that caps DomainBacklogs (topDomainBacklogs, status.go), so
// this mapping only forwards the flag, and BuildLiveBundle is the single
// place that turns it into Bounds.Truncated (#4045 review: a capped domain
// list must not read as a complete one).
func liveEvidenceSnapshotFromReport(report status.Report, repoCount int) evidencebundle.LiveSnapshot {
	blockedByDomain := queueBlockageCountsByDomain(report.QueueBlockages)
	domains := make([]evidencebundle.LiveDomainBacklogSnapshot, 0, len(report.DomainBacklogs))
	for _, backlog := range report.DomainBacklogs {
		buckets := domainBacklogBuckets(backlog, blockedByDomain[backlog.Domain])
		domains = append(domains, evidencebundle.LiveDomainBacklogSnapshot{
			Domain:      backlog.Domain,
			Outstanding: buckets.Outstanding,
			InFlight:    buckets.InFlight,
			Blocked:     buckets.Blocked,
			Retrying:    buckets.Retrying,
			Failed:      buckets.Failed,
			DeadLetter:  buckets.DeadLetter,
			OldestAgeS:  backlog.OldestAge.Seconds(),
		})
	}

	stages := make([]evidencebundle.LiveStageSummarySnapshot, 0, len(report.StageSummaries))
	for _, stage := range report.StageSummaries {
		stages = append(stages, evidencebundle.LiveStageSummarySnapshot{
			Stage:      stage.Stage,
			Pending:    stage.Pending,
			Claimed:    stage.Claimed,
			Running:    stage.Running,
			Retrying:   stage.Retrying,
			Succeeded:  stage.Succeeded,
			Failed:     stage.Failed,
			DeadLetter: stage.DeadLetter,
		})
	}

	runtimeCollectors := status.CollectorRuntimeStatuses(report)
	collectors := make([]evidencebundle.LiveCollectorSnapshot, 0, len(runtimeCollectors))
	for _, collector := range runtimeCollectors {
		collectors = append(collectors, evidencebundle.LiveCollectorSnapshot{
			CollectorKind:  collector.CollectorKind,
			StatusCategory: collector.StatusCategory,
			Health:         collector.Health,
		})
	}

	profiles := make([]evidencebundle.LiveSemanticProviderProfileSnapshot, 0, len(report.SemanticExtraction.ProviderProfiles))
	for _, profile := range report.SemanticExtraction.ProviderProfiles {
		profiles = append(profiles, evidencebundle.LiveSemanticProviderProfileSnapshot{
			ProfileID:    profile.ProfileID,
			ProviderKind: profile.ProviderKind,
			State:        profile.State,
			Reason:       profile.Reason,
		})
	}

	return evidencebundle.LiveSnapshot{
		RepositoryCount:   repoCount,
		HealthState:       report.Health.State,
		HealthReasons:     append([]string(nil), report.Health.Reasons...),
		QueueBlockedCount: sumQueueBlockedCounts(report.QueueBlockages),
		Queue: evidencebundle.LiveQueueSnapshot{
			Total:                 report.Queue.Total,
			Outstanding:           report.Queue.Outstanding,
			OverdueClaims:         report.Queue.OverdueClaims,
			OldestOutstandingAgeS: report.Queue.OldestOutstandingAge.Seconds(),
			Pending:               report.Queue.Pending,
			InFlight:              report.Queue.InFlight,
			Retrying:              report.Queue.Retrying,
			Succeeded:             report.Queue.Succeeded,
			Failed:                report.Queue.Failed,
			DeadLetter:            report.Queue.DeadLetter,
		},
		ScopeActivity: evidencebundle.LiveScopeActivitySnapshot{
			Active:    report.ScopeActivity.Active,
			Changed:   report.ScopeActivity.Changed,
			Unchanged: report.ScopeActivity.Unchanged,
		},
		GenerationHistory:       evidencebundle.LiveGenerationHistorySnapshot(report.GenerationHistory),
		StageSummaries:          stages,
		DomainBacklogs:          domains,
		DomainBacklogsTruncated: report.DomainBacklogsTruncated,
		Collectors:              collectors,
		SemanticExtraction: evidencebundle.LiveSemanticExtractionSnapshot{
			State:              report.SemanticExtraction.State,
			Reason:             report.SemanticExtraction.Reason,
			ProviderConfigured: report.SemanticExtraction.ProviderConfigured,
			ProviderProfiles:   profiles,
		},
	}
}

// sumQueueBlockedCounts totals the gated-row counts across every queue
// blockage entry. This is a different statistic from a single domain's
// blocked bucket (domainBacklogBuckets, aws_materialization_status.go),
// which reports the MAX among that domain's own blockage rows; the two
// answer different questions -- how much work is gated overall, versus which
// domain is worst -- and are not meant to reconcile (see
// evidencebundle.PipelineDomainBacklogSnapshot's Blocked doc). It mirrors
// internal/cli/evbundle's countBlockedQueueEntries (status.go), operating on
// the typed status.QueueBlockage this package already has instead of a
// JSON-decoded copy.
func sumQueueBlockedCounts(blockages []status.QueueBlockage) int {
	total := 0
	for _, blockage := range blockages {
		if blockage.Blocked > 0 {
			total += blockage.Blocked
		}
	}
	return total
}
