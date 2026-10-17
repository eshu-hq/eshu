// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/evidencebundle"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
	"github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
	"github.com/eshu-hq/eshu/go/internal/status/queue"
)

// Fixed bodies of the two failure steps on GET /api/v0/evidence/bundle
// (#7674): the status snapshot read and the bundle validation. The status
// snapshot reads through the guarded PostgreSQL reader, so a stale or
// timed-out reader answers the retryable 503 under
// liveEvidenceBundleCapability, which the error envelope names (#8227).
// Neither error reaches the body; each goes on the request span.
const (
	// liveEvidenceBundleCapability is the capability-matrix key a reader-fence
	// 503 on GET /api/v0/evidence/bundle names in error.capability.
	liveEvidenceBundleCapability = "operations.evidence_bundle_live"

	liveEvidenceBundleStatusReadFailedMessage = "live evidence bundle status read failed"
	liveEvidenceBundleComposeFailedMessage    = "live evidence bundle composition failed"
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
// /api/v0/status/pipeline (openapi/paths/status/routes.go). AuthMiddleware always
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
func (h *Handler) getLiveEvidenceBundle(w http.ResponseWriter, r *http.Request) {
	if h.StatusReader == nil {
		querycontract.WriteError(w, http.StatusServiceUnavailable, "status reader not configured")
		return
	}

	// Retain the existing collector fact and registry selections. The live
	// bundle does not expose Terraform-state serials or warnings, so omit only
	// those reads; required status reads and their errors remain unchanged.
	selection := status.FullSnapshotSelection()
	selection.SkipTerraformStateEvidence = true
	_, report, err := loadStatusReportFiltered(r.Context(), h.StatusReader, time.Now(), status.DefaultOptions(), selection)
	if err != nil {
		if querycontract.WriteGraphReadError(w, r, err, liveEvidenceBundleCapability) {
			return
		}
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, liveEvidenceBundleStatusReadFailedMessage)
		return
	}

	snapshot := liveEvidenceSnapshotFromReport(report, repositoryCountForEvidenceBundle(r, h.Neo4j))
	bundle := evidencebundle.BuildLiveBundle(snapshot, evidencebundle.LiveBundleOptions{
		CreatedAt: time.Now(),
	})
	if err := evidencebundle.Validate(bundle); err != nil {
		// This is a composition bug on our own inputs, never on caller input,
		// so it is a 500, not a 400.
		tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, liveEvidenceBundleComposeFailedMessage)
		return
	}
	bundle = evidencebundle.StampValidation(bundle)
	querycontract.WriteJSON(w, http.StatusOK, bundle)
}

// GraphQuery is the concurrent-safe read-only graph traversal surface,
// aliased rather than redeclared. IntVal projects a row integer through the
// shared querycontract helper, aliased as a package-level value.
//
// Both aliases exist so repositoryCountForEvidenceBundle below keeps the
// byte-identical source the queryplan source-coverage manifest pins
// (source-coverage/first.yaml): that manifest hashes the whole function,
// and any Cypher change must flip its source_sha256. The Cypher here is
// unchanged from the root version; do not repoint the call below without
// re-pinning that manifest entry.
type GraphQuery = querycontract.GraphQuery

var IntVal = querycontract.IntVal

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
// collector.RuntimeStatuses (the same call listCollectors makes) for
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

	runtimeCollectors := collector.RuntimeStatuses(report.CollectorEvidence())
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
		ActiveWorkSource:  report.ActiveWorkSource.JSON(),
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
// blocked bucket (domainBacklogBuckets below), which reports the MAX among
// that domain's own blockage rows; the two answer different questions --
// how much work is gated overall, versus which domain is worst -- and are
// not meant to reconcile (see
// evidencebundle.PipelineDomainBacklogSnapshot's Blocked doc). It mirrors
// internal/cli/evbundle's countBlockedQueueEntries (status.go), operating on
// the typed queue.Blockage this package already has instead of a
// JSON-decoded copy.
func sumQueueBlockedCounts(blockages []queue.Blockage) int {
	total := 0
	for _, blockage := range blockages {
		if blockage.Blocked > 0 {
			total += blockage.Blocked
		}
	}
	return total
}

// The helpers below are hoisted copies of root's status plumbing
// (loadStatusReportFiltered in status_mappers.go, domainBacklogBuckets and
// queueBlockageCountsByDomain in aws_materialization_status.go, #6642
// evidence-leaf move). Each is pure over external status/queue types with no
// root dependency, so the copy keeps this package free of an import cycle
// through root's compatibility aliases; root keeps its originals for the
// staying status routes. Keep both copies identical: the bundle snapshot
// must describe the same report the status routes render.

func loadStatusReportFiltered(
	ctx context.Context,
	reader status.Reader,
	asOf time.Time,
	opts status.Options,
	selection status.SnapshotSelection,
) (status.RawSnapshot, status.Report, error) {
	if reader == nil {
		return status.RawSnapshot{}, status.Report{}, fmt.Errorf("status reader is required")
	}
	raw, err := reader.ReadStatusSnapshotFiltered(ctx, asOf.UTC(), selection)
	if err != nil {
		return status.RawSnapshot{}, status.Report{}, fmt.Errorf("read status snapshot: %w", err)
	}
	return raw, status.BuildReport(raw, opts), nil
}

type domainWorkBuckets struct {
	Outstanding int
	Pending     int
	InFlight    int
	Blocked     int
	Retrying    int
	DeadLetter  int
	Failed      int
}

func domainBacklogBuckets(backlog status.DomainBacklog, blocked int) domainWorkBuckets {
	return domainWorkBuckets{
		Outstanding: backlog.Outstanding,
		Pending:     pendingDomainWork(backlog),
		InFlight:    backlog.InFlight,
		Blocked:     blocked,
		Retrying:    backlog.Retrying,
		DeadLetter:  backlog.DeadLetter,
		Failed:      backlog.Failed,
	}
}

func pendingDomainWork(backlog status.DomainBacklog) int {
	pending := backlog.Outstanding - backlog.InFlight - backlog.Retrying
	if pending < 0 {
		return 0
	}
	return pending
}

func queueBlockageCountsByDomain(blockages []queue.Blockage) map[string]int {
	counts := make(map[string]int)
	for _, blockage := range blockages {
		if blockage.Blocked > counts[blockage.Domain] {
			counts[blockage.Domain] = blockage.Blocked
		}
	}
	return counts
}
