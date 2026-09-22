// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/status/cloud"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
	"github.com/eshu-hq/eshu/go/internal/status/generation"
	"github.com/eshu-hq/eshu/go/internal/status/queue"
	"github.com/eshu-hq/eshu/go/internal/status/semantic"
	"github.com/eshu-hq/eshu/go/internal/status/shared"
	"github.com/eshu-hq/eshu/go/internal/status/tfstate"
)

// RenderJSON returns a stable machine-readable projection of the report.
func RenderJSON(report Report) ([]byte, error) {
	payload := struct {
		Version                        string                              `json:"version"`
		AsOf                           string                              `json:"as_of"`
		Health                         HealthSummary                       `json:"health"`
		Coordinator                    *coordinatorSnapshotJSON            `json:"coordinator,omitempty"`
		CollectorRuntimes              []collector.RuntimeStatusJSON       `json:"collector_runtimes,omitempty"`
		CollectorPromotionProofs       []collector.PromotionProofJSON      `json:"collector_promotion_proofs,omitempty"`
		Flow                           []flowSummaryJSON                   `json:"flow"`
		Queue                          queueJSON                           `json:"queue"`
		LatestFailure                  *queueFailureJSON                   `json:"latest_failure,omitempty"`
		RetryPolicies                  []retryPolicyJSON                   `json:"retry_policies"`
		RegistryCollectors             []registryCollectorJSON             `json:"registry_collectors,omitempty"`
		AWSCloudScans                  []cloud.AWSScanJSON                 `json:"aws_cloud_scans,omitempty"`
		AWSFreshness                   *cloud.AWSFreshnessJSON             `json:"aws_freshness,omitempty"`
		InfraInventory                 *infraInventoryJSON                 `json:"infra_inventory,omitempty"`
		VulnerabilitySources           []collector.VulnerabilitySourceJSON `json:"vulnerability_sources,omitempty"`
		SemanticExtraction             semantic.ExtractionJSON             `json:"semantic_extraction"`
		AnswerNarration                answerNarrationJSON                 `json:"answer_narration"`
		CollectorGenerationDeadLetters collector.GenerationDeadLetterJSON  `json:"collector_generation_dead_letters"`
		AWSCloudScansTruncated         bool                                `json:"aws_cloud_scans_truncated,omitempty"`
		AWSCloudScanLimit              int                                 `json:"aws_cloud_scan_limit,omitempty"`
		ScopeActivity                  scopeActivityJSON                   `json:"scope_activity"`
		GenerationHistory              generationHistoryJSON               `json:"generation_history"`
		GenerationTransitions          []generation.TransitionJSON         `json:"generation_transitions"`
		Scopes                         map[string]int                      `json:"scopes"`
		Generations                    map[string]int                      `json:"generations"`
		Stages                         []StageSummary                      `json:"stages"`
		Domains                        []domainBacklogJSON                 `json:"domains"`
		DomainBacklogsTruncated        bool                                `json:"domain_backlogs_truncated,omitempty"`
		DomainBacklogsLimit            int                                 `json:"domain_backlogs_limit,omitempty"`
		QueueBlockages                 []queueBlockageJSON                 `json:"queue_blockages"`
		TerraformState                 *tfstate.ReportJSON                 `json:"terraform_state,omitempty"`
	}{
		Version:           buildinfo.AppVersion(),
		AsOf:              report.AsOf.UTC().Format(time.RFC3339),
		Health:            report.Health,
		Coordinator:       coordinatorJSON(report.Coordinator),
		CollectorRuntimes: collector.RuntimeStatusesJSON(collector.RuntimeStatuses(collectorEvidence(report))),
		CollectorPromotionProofs: collector.PromotionProofsJSON(collector.PromotionProofs(collectorEvidence(report), collector.PromotionOptions{
			Catalog:    collector.PresentCatalog(collectorEvidence(report)),
			AsOf:       report.AsOf,
			StaleAfter: collector.DefaultPromotionStaleAfter,
		})),
		Flow:                           flowSummariesJSON(report.FlowSummaries),
		Queue:                          queueJSONFromReport(report.Queue),
		LatestFailure:                  queueFailureJSONFromReport(report.LatestQueueFailure),
		RetryPolicies:                  retryPoliciesJSON(report.RetryPolicies),
		RegistryCollectors:             registryCollectorsJSON(report.RegistryCollectors),
		AWSCloudScans:                  cloud.AWSScansJSON(report.AWSCloudScans),
		AWSFreshness:                   cloud.AWSFreshnessJSONFrom(report.AWSFreshness),
		InfraInventory:                 infraInventoryJSONFromReport(report.InfraInventory),
		VulnerabilitySources:           collector.VulnerabilitySourcesJSON(report.VulnerabilitySources),
		SemanticExtraction:             semantic.ExtractionStatusJSON(report.SemanticExtraction),
		AnswerNarration:                answerNarrationStatusJSON(report.AnswerNarration),
		CollectorGenerationDeadLetters: collector.GenerationDeadLetterJSONFrom(report.CollectorGenerationDeadLetters),
		AWSCloudScansTruncated:         report.AWSCloudScansTruncated,
		AWSCloudScanLimit:              awsCloudScanLimitJSON(report),
		ScopeActivity:                  scopeActivityJSONFromReport(report.ScopeActivity),
		GenerationHistory:              generationHistoryJSONFromReport(report.GenerationHistory),
		GenerationTransitions:          generation.TransitionsJSON(report.GenerationTransitions),
		Scopes:                         cloneCounts(report.ScopeTotals),
		Generations:                    cloneCounts(report.GenerationTotals),
		Stages:                         slices.Clone(report.StageSummaries),
		Domains:                        domainBacklogsJSON(report.DomainBacklogs),
		DomainBacklogsTruncated:        report.DomainBacklogsTruncated,
		DomainBacklogsLimit:            domainBacklogsLimitJSON(report),
		QueueBlockages:                 queueBlockagesJSON(report.QueueBlockages),
		TerraformState:                 tfstate.ReportJSONFrom(report.TerraformState),
	}

	return json.MarshalIndent(payload, "", "  ")
}

type queueJSON struct {
	Total                                 int     `json:"total"`
	Outstanding                           int     `json:"outstanding"`
	Pending                               int     `json:"pending"`
	InFlight                              int     `json:"in_flight"`
	Retrying                              int     `json:"retrying"`
	Succeeded                             int     `json:"succeeded"`
	Failed                                int     `json:"failed"`
	DeadLetter                            int     `json:"dead_letter"`
	ProvenanceEdgeIdentityUpgradeApplied  bool    `json:"provenance_edge_identity_upgrade_applied"`
	ProvenanceEdgeIdentityUpgradeRequired int     `json:"provenance_edge_identity_upgrade_required"`
	OverdueClaims                         int     `json:"overdue_claims"`
	OldestOutstandingAge                  string  `json:"oldest_outstanding_age"`
	OldestOutstandingAgeSeconds           float64 `json:"oldest_outstanding_age_seconds"`
}

type queueFailureJSON struct {
	Stage          string `json:"stage"`
	Domain         string `json:"domain"`
	Status         string `json:"status"`
	WorkItemID     string `json:"work_item_id,omitempty"`
	ScopeID        string `json:"scope_id,omitempty"`
	GenerationID   string `json:"generation_id,omitempty"`
	FailureClass   string `json:"failure_class"`
	FailureMessage string `json:"failure_message,omitempty"`
	FailureDetails string `json:"failure_details,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
}

type scopeActivityJSON struct {
	Active    int `json:"active"`
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
}

type generationHistoryJSON struct {
	Active     int `json:"active"`
	Pending    int `json:"pending"`
	Completed  int `json:"completed"`
	Superseded int `json:"superseded"`
	Failed     int `json:"failed"`
	Other      int `json:"other"`
}

type collectorInstanceJSON struct {
	InstanceID     string  `json:"instance_id"`
	CollectorKind  string  `json:"collector_kind"`
	Mode           string  `json:"mode"`
	Enabled        bool    `json:"enabled"`
	Bootstrap      bool    `json:"bootstrap"`
	ClaimsEnabled  bool    `json:"claims_enabled"`
	DisplayName    string  `json:"display_name,omitempty"`
	LastObservedAt string  `json:"last_observed_at"`
	UpdatedAt      string  `json:"updated_at"`
	DeactivatedAt  *string `json:"deactivated_at,omitempty"`
}

type coordinatorSnapshotJSON struct {
	CollectorInstances    []collectorInstanceJSON        `json:"collector_instances"`
	RunStatusCounts       []shared.NamedCountJSON        `json:"run_status_counts"`
	WorkItemStatusCounts  []shared.NamedCountJSON        `json:"work_item_status_counts"`
	CompletenessCounts    []shared.NamedCountJSON        `json:"completeness_counts"`
	CollectorBackpressure []collector.BackpressureJSON   `json:"collector_backpressure,omitempty"`
	ActiveClaims          int                            `json:"active_claims"`
	OverdueClaims         int                            `json:"overdue_claims"`
	OldestPendingAge      string                         `json:"oldest_pending_age"`
	OldestPendingSeconds  float64                        `json:"oldest_pending_age_seconds"`
	RecentFailures        *coordinatorRecentFailuresJSON `json:"recent_failures,omitempty"`
}

type coordinatorRecentFailuresJSON struct {
	Window              string  `json:"window"`
	WindowSeconds       float64 `json:"window_seconds"`
	FailedRuns          int     `json:"failed_runs"`
	BlockedCompleteness int     `json:"blocked_completeness"`
	TerminalWorkItems   int     `json:"terminal_work_items"`
}

type registryCollectorJSON struct {
	CollectorKind              string                  `json:"collector_kind"`
	ConfiguredInstances        int                     `json:"configured_instances"`
	ActiveScopes               int                     `json:"active_scopes"`
	RecentCompletedGenerations int                     `json:"recent_completed_generations"`
	LastCompletedAt            string                  `json:"last_completed_at,omitempty"`
	RetryableFailures          int                     `json:"retryable_failures"`
	TerminalFailures           int                     `json:"terminal_failures"`
	FailureClassCounts         []shared.NamedCountJSON `json:"failure_class_counts,omitempty"`
	MetadataTargets            []metadataTargetJSON    `json:"metadata_targets,omitempty"`
}

type metadataTargetJSON struct {
	Ecosystem   string `json:"ecosystem"`
	Planned     int    `json:"planned"`
	Completed   int    `json:"completed"`
	Skipped     int    `json:"skipped"`
	Stale       int    `json:"stale"`
	Failed      int    `json:"failed"`
	RateLimited int    `json:"rate_limited"`
}

type domainBacklogJSON struct {
	Domain           string  `json:"domain"`
	Outstanding      int     `json:"outstanding"`
	InFlight         int     `json:"in_flight"`
	Retrying         int     `json:"retrying"`
	Failed           int     `json:"failed"`
	DeadLetter       int     `json:"dead_letter"`
	OldestAge        string  `json:"oldest_age"`
	OldestAgeSeconds float64 `json:"oldest_age_seconds"`
}

type queueBlockageJSON struct {
	Stage            string  `json:"stage"`
	Domain           string  `json:"domain"`
	ConflictDomain   string  `json:"conflict_domain"`
	ConflictKey      string  `json:"conflict_key"`
	Blocked          int     `json:"blocked"`
	OldestAge        string  `json:"oldest_age"`
	OldestAgeSeconds float64 `json:"oldest_age_seconds"`
}

func queueJSONFromReport(snapshot QueueSnapshot) queueJSON {
	return queueJSON{
		Total:                                 snapshot.Total,
		Outstanding:                           snapshot.Outstanding,
		Pending:                               snapshot.Pending,
		InFlight:                              snapshot.InFlight,
		Retrying:                              snapshot.Retrying,
		Succeeded:                             snapshot.Succeeded,
		Failed:                                snapshot.Failed,
		DeadLetter:                            snapshot.DeadLetter,
		ProvenanceEdgeIdentityUpgradeApplied:  snapshot.ProvenanceEdgeIdentityUpgradeApplied,
		ProvenanceEdgeIdentityUpgradeRequired: snapshot.ProvenanceEdgeIdentityUpgradeRequired,
		OverdueClaims:                         snapshot.OverdueClaims,
		OldestOutstandingAge:                  snapshot.OldestOutstandingAge.String(),
		OldestOutstandingAgeSeconds:           snapshot.OldestOutstandingAge.Seconds(),
	}
}

func queueFailureJSONFromReport(snapshot *queue.FailureSnapshot) *queueFailureJSON {
	if snapshot == nil {
		return nil
	}

	return &queueFailureJSON{
		Stage:          snapshot.Stage,
		Domain:         snapshot.Domain,
		Status:         snapshot.Status,
		WorkItemID:     snapshot.WorkItemID,
		ScopeID:        snapshot.ScopeID,
		GenerationID:   snapshot.GenerationID,
		FailureClass:   snapshot.FailureClass,
		FailureMessage: snapshot.FailureMessage,
		FailureDetails: snapshot.FailureDetails,
		UpdatedAt:      shared.NullableRFC3339Value(snapshot.UpdatedAt),
	}
}

func scopeActivityJSONFromReport(scopeActivity ScopeActivitySnapshot) scopeActivityJSON {
	return scopeActivityJSON(scopeActivity)
}

func domainBacklogsJSON(rows []DomainBacklog) []domainBacklogJSON {
	projected := make([]domainBacklogJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, domainBacklogJSON{
			Domain:           row.Domain,
			Outstanding:      row.Outstanding,
			InFlight:         row.InFlight,
			Retrying:         row.Retrying,
			Failed:           row.Failed,
			DeadLetter:       row.DeadLetter,
			OldestAge:        row.OldestAge.String(),
			OldestAgeSeconds: row.OldestAge.Seconds(),
		})
	}

	return projected
}

func queueBlockagesJSON(rows []queue.Blockage) []queueBlockageJSON {
	projected := make([]queueBlockageJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, queueBlockageJSON{
			Stage:            row.Stage,
			Domain:           row.Domain,
			ConflictDomain:   row.ConflictDomain,
			ConflictKey:      row.ConflictKey,
			Blocked:          row.Blocked,
			OldestAge:        row.OldestAge.String(),
			OldestAgeSeconds: row.OldestAge.Seconds(),
		})
	}

	return projected
}

func coordinatorJSON(snapshot *CoordinatorSnapshot) *coordinatorSnapshotJSON {
	if snapshot == nil {
		return nil
	}

	instances := make([]collectorInstanceJSON, 0, len(snapshot.CollectorInstances))
	for _, instance := range snapshot.CollectorInstances {
		instances = append(instances, collectorInstanceJSON{
			InstanceID:     instance.InstanceID,
			CollectorKind:  instance.CollectorKind,
			Mode:           instance.Mode,
			Enabled:        instance.Enabled,
			Bootstrap:      instance.Bootstrap,
			ClaimsEnabled:  instance.ClaimsEnabled,
			DisplayName:    instance.DisplayName,
			LastObservedAt: instance.LastObservedAt.UTC().Format(time.RFC3339),
			UpdatedAt:      instance.UpdatedAt.UTC().Format(time.RFC3339),
			DeactivatedAt:  nullableRFC3339String(instance.DeactivatedAt),
		})
	}

	return &coordinatorSnapshotJSON{
		CollectorInstances:    instances,
		RunStatusCounts:       shared.NamedCountsJSON(snapshot.RunStatusCounts),
		WorkItemStatusCounts:  shared.NamedCountsJSON(snapshot.WorkItemStatusCounts),
		CompletenessCounts:    shared.NamedCountsJSON(snapshot.CompletenessCounts),
		CollectorBackpressure: collector.BackpressureJSONRows(snapshot.CollectorBackpressure),
		ActiveClaims:          snapshot.ActiveClaims,
		OverdueClaims:         snapshot.OverdueClaims,
		OldestPendingAge:      snapshot.OldestPendingAge.String(),
		OldestPendingSeconds:  snapshot.OldestPendingAge.Seconds(),
		RecentFailures:        coordinatorRecentFailuresJSONValue(snapshot.RecentFailures),
	}
}

func coordinatorRecentFailuresJSONValue(recent *CoordinatorRecentFailures) *coordinatorRecentFailuresJSON {
	if recent == nil {
		return nil
	}
	return &coordinatorRecentFailuresJSON{
		Window:              recent.Window.String(),
		WindowSeconds:       recent.Window.Seconds(),
		FailedRuns:          recent.FailedRuns,
		BlockedCompleteness: recent.BlockedCompleteness,
		TerminalWorkItems:   recent.TerminalWorkItems,
	}
}

func registryCollectorsJSON(rows []RegistryCollectorSnapshot) []registryCollectorJSON {
	projected := make([]registryCollectorJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, registryCollectorJSON{
			CollectorKind:              row.CollectorKind,
			ConfiguredInstances:        row.ConfiguredInstances,
			ActiveScopes:               row.ActiveScopes,
			RecentCompletedGenerations: row.RecentCompletedGenerations,
			LastCompletedAt:            shared.NullableRFC3339Value(row.LastCompletedAt),
			RetryableFailures:          row.RetryableFailures,
			TerminalFailures:           row.TerminalFailures,
			FailureClassCounts:         shared.NamedCountsJSON(row.FailureClassCounts),
			MetadataTargets:            metadataTargetsJSON(row.MetadataTargetCounts),
		})
	}
	return projected
}

func metadataTargetsJSON(rows []RegistryMetadataTargetCount) []metadataTargetJSON {
	projected := make([]metadataTargetJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, metadataTargetJSON(row))
	}
	return projected
}

func awsCloudScanLimitJSON(report Report) int {
	if !report.AWSCloudScansTruncated {
		return 0
	}
	return report.AWSCloudScanLimit
}

func domainBacklogsLimitJSON(report Report) int {
	if !report.DomainBacklogsTruncated {
		return 0
	}
	return report.DomainBacklogsLimit
}

func nullableRFC3339String(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}
