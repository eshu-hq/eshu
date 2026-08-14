// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/cli/scan"
	"github.com/eshu-hq/eshu/go/internal/evidencebundle"
)

const (
	evidenceBundleIndexEndpoint      = "/api/v0/status/index"
	evidenceBundlePipelineEndpoint   = "/api/v0/status/pipeline"
	evidenceBundleCollectorsEndpoint = "/api/v0/status/collectors"
)

// evidenceBundleLiveNow is time.Now, overridable in tests for a deterministic
// Identity.CreatedAt.
var evidenceBundleLiveNow = time.Now

// evidenceIndexStatus decodes the subset of GET /api/v0/status/index this
// command needs: repository count, queue blockage count, and semantic/
// provider status (see internal/query/status.go getIndexStatus).
type evidenceIndexStatus struct {
	RepositoryCount    int                             `json:"repository_count"`
	QueueBlockages     []evidenceQueueBlockage         `json:"queue_blockages"`
	SemanticExtraction evidenceSemanticExtractionState `json:"semantic_extraction"`
}

// evidenceQueueBlockage decodes one queue_blockages entry. status.QueueBlockage
// carries Blocked as an int count of gated rows, not a flag, so the API
// serializes a number here (go/internal/query/status_mappers.go). Decoding it
// as a bool aborted the export with a json error precisely when blockage
// evidence existed.
type evidenceQueueBlockage struct {
	Blocked int `json:"blocked"`
}

// evidenceSemanticExtractionState decodes the semantic_extraction block
// shared by GET /api/v0/status/index and GET /api/v0/status/pipeline (see
// internal/query/status_semantic_extraction.go semanticExtractionStatusToMap).
type evidenceSemanticExtractionState struct {
	State              string                            `json:"state"`
	Reason             string                            `json:"reason"`
	ProviderConfigured bool                              `json:"provider_configured"`
	ProviderProfiles   []evidenceSemanticProviderProfile `json:"provider_profiles"`
}

// evidenceSemanticProviderProfile decodes one provider_profiles entry.
type evidenceSemanticProviderProfile struct {
	ProfileID    string `json:"profile_id"`
	ProviderKind string `json:"provider_kind"`
	State        string `json:"state"`
	Reason       string `json:"reason"`
}

// evidenceCollectorsResponse decodes GET /api/v0/status/collectors (see
// internal/query/status.go listCollectors).
type evidenceCollectorsResponse struct {
	Collectors []evidenceCollector `json:"collectors"`
}

// evidenceCollector decodes one collector readiness entry; only the
// classification fields are needed, never an instance address or endpoint.
type evidenceCollector struct {
	CollectorKind  string `json:"collector_kind"`
	StatusCategory string `json:"status_category"`
	Health         string `json:"health"`
}

// runEvidenceBundleExportLive fetches the running stack's status/index,
// status/pipeline, and status/collectors routes, composes a live evidence
// bundle from them via the pure evidencebundle.BuildLiveBundle, validates
// it, and writes it the same way the demo path does.
func runEvidenceBundleExportLive(cmd *cobra.Command, scope, outPath string) error {
	client := apiClientFromCmd(cmd)
	snapshot, err := fetchLiveEvidenceSnapshot(client)
	if err != nil {
		return err
	}

	bundle := evidencebundle.BuildLiveBundle(snapshot, evidencebundle.LiveBundleOptions{
		ScopeID:   scope,
		CreatedAt: evidenceBundleLiveNow(),
	})
	if err := evidencebundle.Validate(bundle); err != nil {
		return fmt.Errorf("validate live evidence bundle: %w", err)
	}
	// Stamp only now that Validate actually returned nil.
	bundle = evidencebundle.StampValidation(bundle)
	raw, err := evidencebundle.RenderJSON(bundle)
	if err != nil {
		return err
	}
	return writeEvidenceBundleOutput(cmd, raw, outPath)
}

// fetchLiveEvidenceSnapshot performs the three status-route GETs and maps
// their decoded responses into an evidencebundle.LiveSnapshot. This is the
// only place in the live-export path that dials the network; evidencebundle
// itself stays a pure composer (see its README.md Ownership boundary).
func fetchLiveEvidenceSnapshot(client *APIClient) (evidencebundle.LiveSnapshot, error) {
	var index evidenceIndexStatus
	if err := client.Get(evidenceBundleIndexEndpoint, &index); err != nil {
		return evidencebundle.LiveSnapshot{}, fmt.Errorf("fetch %s: %w", evidenceBundleIndexEndpoint, err)
	}
	var pipeline scan.PipelineStatus
	if err := client.Get(evidenceBundlePipelineEndpoint, &pipeline); err != nil {
		return evidencebundle.LiveSnapshot{}, fmt.Errorf("fetch %s: %w", evidenceBundlePipelineEndpoint, err)
	}
	var collectors evidenceCollectorsResponse
	if err := client.Get(evidenceBundleCollectorsEndpoint, &collectors); err != nil {
		return evidencebundle.LiveSnapshot{}, fmt.Errorf("fetch %s: %w", evidenceBundleCollectorsEndpoint, err)
	}
	return liveEvidenceSnapshotFromStatus(index, pipeline, collectors), nil
}

func liveEvidenceSnapshotFromStatus(
	index evidenceIndexStatus,
	pipeline scan.PipelineStatus,
	collectors evidenceCollectorsResponse,
) evidencebundle.LiveSnapshot {
	stages := make([]evidencebundle.LiveStageSummarySnapshot, 0, len(pipeline.StageSummaries))
	for _, stage := range pipeline.StageSummaries {
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
	domains := make([]evidencebundle.LiveDomainBacklogSnapshot, 0, len(pipeline.DomainBacklogs))
	for _, domain := range pipeline.DomainBacklogs {
		domains = append(domains, evidencebundle.LiveDomainBacklogSnapshot{
			Domain:      domain.Domain,
			Outstanding: domain.Outstanding,
			InFlight:    domain.InFlight,
			Blocked:     domain.Blocked,
			Retrying:    domain.Retrying,
			Failed:      domain.Failed,
			DeadLetter:  domain.DeadLetter,
			OldestAgeS:  domain.OldestAgeS,
		})
	}
	liveCollectors := make([]evidencebundle.LiveCollectorSnapshot, 0, len(collectors.Collectors))
	for _, collector := range collectors.Collectors {
		liveCollectors = append(liveCollectors, evidencebundle.LiveCollectorSnapshot{
			CollectorKind:  collector.CollectorKind,
			StatusCategory: collector.StatusCategory,
			Health:         collector.Health,
		})
	}
	profiles := make([]evidencebundle.LiveSemanticProviderProfileSnapshot, 0, len(index.SemanticExtraction.ProviderProfiles))
	for _, profile := range index.SemanticExtraction.ProviderProfiles {
		profiles = append(profiles, evidencebundle.LiveSemanticProviderProfileSnapshot{
			ProfileID:    profile.ProfileID,
			ProviderKind: profile.ProviderKind,
			State:        profile.State,
			Reason:       profile.Reason,
		})
	}

	return evidencebundle.LiveSnapshot{
		RepositoryCount:   index.RepositoryCount,
		HealthState:       pipeline.Health.State,
		HealthReasons:     append([]string(nil), pipeline.Health.Reasons...),
		QueueBlockedCount: countBlockedQueueEntries(index.QueueBlockages),
		Queue: evidencebundle.LiveQueueSnapshot{
			Total:                 pipeline.Queue.Total,
			Outstanding:           pipeline.Queue.Outstanding,
			OverdueClaims:         pipeline.Queue.OverdueClaims,
			OldestOutstandingAgeS: pipeline.Queue.OldestOutstandingAgeS,
			Pending:               pipeline.Queue.Pending,
			InFlight:              pipeline.Queue.InFlight,
			Retrying:              pipeline.Queue.Retrying,
			Succeeded:             pipeline.Queue.Succeeded,
			Failed:                pipeline.Queue.Failed,
			DeadLetter:            pipeline.Queue.DeadLetter,
		},
		ScopeActivity: evidencebundle.LiveScopeActivitySnapshot{
			Active:    pipeline.ScopeActivity.Active,
			Changed:   pipeline.ScopeActivity.Changed,
			Unchanged: pipeline.ScopeActivity.Unchanged,
		},
		GenerationHistory: evidencebundle.LiveGenerationHistorySnapshot(pipeline.GenerationHistory),
		StageSummaries:    stages,
		DomainBacklogs:    domains,
		Collectors:        liveCollectors,
		SemanticExtraction: evidencebundle.LiveSemanticExtractionSnapshot{
			State:              index.SemanticExtraction.State,
			Reason:             index.SemanticExtraction.Reason,
			ProviderConfigured: index.SemanticExtraction.ProviderConfigured,
			ProviderProfiles:   profiles,
		},
	}
}

// countBlockedQueueEntries sums the gated-row counts across blockage entries.
//
// This is a different statistic from domain_backlogs[].blocked, which
// /status/pipeline reports as the maximum among a single domain's blockage
// rows (queueBlockageCountsByDomain). The two will not add up; neither is
// wrong, and the bundle carries both because they answer different questions --
// how much work is gated overall, and which domain is worst.
// Blocked is a row count rather than a flag, so summing reports how much work
// is gated; counting entries would under-report a single heavily-gated domain
// as 1.
func countBlockedQueueEntries(blockages []evidenceQueueBlockage) int {
	total := 0
	for _, blockage := range blockages {
		if blockage.Blocked > 0 {
			total += blockage.Blocked
		}
	}
	return total
}
