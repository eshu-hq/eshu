// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// scopedOperatorControlPlaneRoute reports whether the request targets the
// operator read model. The handler redacts raw correlation IDs and
// instance-level labels for scoped tokens, so the route is tenant-filter safe.
func scopedOperatorControlPlaneRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/status/operator-control-plane"
}

// operatorControlPlaneDomainLimit caps the reducer-domain rows the operator
// read model projects. It is set above the bounded reducer-domain catalog
// (~52 domains) so a dead-lettered domain with low outstanding work is never
// dropped by the default top-N backlog cap; surfacing every dead letter is the
// point of this read model.
const operatorControlPlaneDomainLimit = 256

// getOperatorControlPlane returns the unified operator read model: queue
// pressure with claim-latency and stuck-work signals, reducer-domain backlogs,
// collector-family promotion verdicts with the newest proof artifact, and
// dead-letter state classed by reducer domain and collector-generation commit.
// It loads exactly one status snapshot (the same read path as
// /api/v0/status/pipeline) and projects it in memory, adding no database cost.
//
// Scoped tokens receive the same aggregate counts with raw correlation IDs and
// instance-scoped labels redacted; shared tokens see the full read model.
func (h *StatusHandler) getOperatorControlPlane(w http.ResponseWriter, r *http.Request) {
	if h.StatusReader == nil {
		WriteError(w, http.StatusServiceUnavailable, "status reader not configured")
		return
	}

	opts := status.DefaultOptions()
	opts.DomainLimit = operatorControlPlaneDomainLimit
	report, err := status.LoadReport(r.Context(), h.StatusReader, time.Now(), opts)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, fmt.Sprintf("load status: %v", err))
		return
	}

	cp := status.ControlPlane(report)
	WriteJSON(w, http.StatusOK, operatorControlPlaneToMap(cp, scopedAuthContext(r.Context())))
}

// operatorControlPlaneToMap renders the read model to a JSON-friendly map. When
// scoped is true, raw work-item/scope/generation identifiers and instance-level
// labels are withheld while every aggregate count and age is preserved.
func operatorControlPlaneToMap(cp status.OperatorControlPlane, scoped bool) map[string]any {
	return map[string]any{
		"version":            buildinfo.AppVersion(),
		"as_of":              cp.AsOf.Format(time.RFC3339),
		"scoped":             scoped,
		"health":             healthToMap(cp.Health),
		"queue":              operatorQueueToMap(cp.Queue),
		"reducer_domains":    operatorReducerDomainsToSlice(cp.ReducerDomains),
		"collector_families": operatorCollectorFamiliesToSlice(cp.CollectorFamilies, scoped),
		"dead_letters":       operatorDeadLettersToMap(cp.DeadLetters, scoped),
		"retry_policies":     retryPoliciesToSlice(cp.RetryPolicies),
	}
}

func operatorQueueToMap(q status.OperatorQueueView) map[string]any {
	return map[string]any{
		"total":       q.Total,
		"outstanding": q.Outstanding,
		"pending":     q.Pending,
		"in_flight":   q.InFlight,
		"retrying":    q.Retrying,
		"dead_letter": q.DeadLetter,
		"claim_latency": map[string]any{
			"overdue_claims":             q.OverdueClaims,
			"oldest_outstanding_age":     q.OldestOutstandingAge.Seconds(),
			"oldest_outstanding_age_ms":  q.OldestOutstandingAge.Milliseconds(),
			"coordinator_oldest_pending": q.OldestPendingAge.Seconds(),
		},
		"stuck": map[string]any{
			"oldest_outstanding_age": q.OldestOutstandingAge.Seconds(),
			"blocked_conflicts":      q.BlockedConflicts,
		},
	}
}

func operatorReducerDomainsToSlice(domains []status.DomainBacklog) []map[string]any {
	result := make([]map[string]any, 0, len(domains))
	for _, d := range domains {
		result = append(result, map[string]any{
			"domain":      d.Domain,
			"outstanding": d.Outstanding,
			"in_flight":   d.InFlight,
			"retrying":    d.Retrying,
			"dead_letter": d.DeadLetter,
			"failed":      d.Failed,
			"oldest_age":  d.OldestAge.Seconds(),
		})
	}
	return result
}

func operatorCollectorFamiliesToSlice(families []status.OperatorCollectorFamily, scoped bool) []map[string]any {
	result := make([]map[string]any, 0, len(families))
	for _, fam := range families {
		row := map[string]any{
			"collector_kind":   fam.CollectorKind,
			"promotion_state":  fam.PromotionState,
			"health":           fam.Health,
			"claim_state":      fam.ClaimState,
			"reducer_readback": fam.ReducerReadback,
			"last_observed_at": nullableRFC3339(fam.LastObservedAt),
			"telemetry":        fam.TelemetryHandles,
		}
		// Family display names and blockers can echo instance-specific labels;
		// withhold them from scoped callers while keeping the verdict.
		if !scoped {
			row["display_name"] = fam.DisplayName
			row["blockers"] = fam.Blockers
		}
		result = append(result, row)
	}
	return result
}

func operatorDeadLettersToMap(dead status.OperatorDeadLetters, scoped bool) map[string]any {
	byDomain := make([]map[string]any, 0, len(dead.ByDomain))
	for _, d := range dead.ByDomain {
		byDomain = append(byDomain, map[string]any{
			"domain":      d.Domain,
			"dead_letter": d.DeadLetter,
			"oldest_age":  d.OldestAge.Seconds(),
		})
	}

	// Emit latest_failure only when a failure exists so consumers do not read an
	// all-empty object as a failure record.
	var latest any
	if dead.LatestFailureClass != "" || !dead.LatestFailureAt.IsZero() {
		fields := map[string]any{
			"failure_class": dead.LatestFailureClass,
			"domain":        dead.LatestFailureDomain,
			"at":            nullableRFC3339(dead.LatestFailureAt),
		}
		// Raw queue/scope/generation identifiers correlate to internal rows and
		// are withheld from scoped callers; the class and domain stay visible.
		if !scoped {
			fields["work_item_id"] = dead.LatestFailureWorkItemID
			fields["scope_id"] = dead.LatestFailureScopeID
			fields["generation_id"] = dead.LatestFailureGenerationID
		}
		latest = fields
	}

	return map[string]any{
		"queue_dead_letter":    dead.QueueDeadLetter,
		"by_domain":            byDomain,
		"collector_generation": collectorGenerationDeadLettersToMap(dead.CollectorGeneration),
		"latest_failure":       latest,
	}
}
