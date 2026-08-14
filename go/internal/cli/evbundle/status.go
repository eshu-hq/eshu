// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evbundle

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/evidencebundle"
)

// The three status routes a live bundle is composed from. All three are
// stack-global: /status/index counts every repository, and neither the
// pipeline nor the collectors route takes a scope selector.
const (
	// IndexEndpoint reports repository count, queue blockages, and semantic
	// extraction state.
	IndexEndpoint = "/api/v0/status/index"
	// PipelineEndpoint reports health, queue depth, generation history, stage
	// summaries, domain backlogs, and scope activity.
	PipelineEndpoint = "/api/v0/status/pipeline"
	// CollectorsEndpoint reports per-collector readiness classification.
	CollectorsEndpoint = "/api/v0/status/collectors"
)

// StatusFetcher is the one network capability this package needs: an
// authenticated GET against a status route that decodes the JSON body into
// result. cmd/eshu's *APIClient satisfies it. The interface is declared here,
// at the point of use, so this package never depends on how the caller
// resolves a base URL, an API key, or a timeout.
type StatusFetcher interface {
	Get(path string, result any) error
}

// IndexStatus decodes the subset of GET /api/v0/status/index a bundle needs:
// repository count, queue blockage counts, and semantic provider state (see
// internal/query/status.go getIndexStatus).
type IndexStatus struct {
	RepositoryCount    int                     `json:"repository_count"`
	QueueBlockages     []QueueBlockage         `json:"queue_blockages"`
	SemanticExtraction SemanticExtractionState `json:"semantic_extraction"`
}

// QueueBlockage decodes one queue_blockages entry. status.QueueBlockage
// carries Blocked as an int count of gated rows, not a flag, so the API
// serializes a number here (go/internal/query/status_mappers.go). Decoding it
// as a bool aborted the export with a json error precisely when blockage
// evidence existed.
type QueueBlockage struct {
	Blocked int `json:"blocked"`
}

// SemanticExtractionState decodes the semantic_extraction block shared by
// GET /api/v0/status/index and GET /api/v0/status/pipeline (see
// internal/query/status_semantic_extraction.go semanticExtractionStatusToMap).
type SemanticExtractionState struct {
	State              string                    `json:"state"`
	Reason             string                    `json:"reason"`
	ProviderConfigured bool                      `json:"provider_configured"`
	ProviderProfiles   []SemanticProviderProfile `json:"provider_profiles"`
}

// SemanticProviderProfile decodes one provider_profiles entry.
type SemanticProviderProfile struct {
	ProfileID    string `json:"profile_id"`
	ProviderKind string `json:"provider_kind"`
	State        string `json:"state"`
	Reason       string `json:"reason"`
}

// CollectorsResponse decodes GET /api/v0/status/collectors (see
// internal/query/status.go listCollectors).
type CollectorsResponse struct {
	Collectors []Collector `json:"collectors"`
}

// Collector decodes one collector readiness entry; only the classification
// fields are needed, never an instance address or endpoint.
type Collector struct {
	CollectorKind  string `json:"collector_kind"`
	StatusCategory string `json:"status_category"`
	Health         string `json:"health"`
}

// PipelineStatus decodes the subset of GET /api/v0/status/pipeline a bundle
// needs. It is deliberately this package's own decode type rather than a
// shared one: internal/cli/scan's PipelineStatus serves the scan, first-run,
// and hosted-setup families, which read a different subset and must stay free
// to change without moving what a share-safe artifact reports.
type PipelineStatus struct {
	Health            PipelineHealth            `json:"health,omitempty"`
	Queue             PipelineQueue             `json:"queue,omitempty"`
	GenerationHistory PipelineGenerationHistory `json:"generation_history,omitempty"`
	StageSummaries    []PipelineStageSummary    `json:"stage_summaries,omitempty"`
	DomainBacklogs    []PipelineDomainBacklog   `json:"domain_backlogs,omitempty"`
	ScopeActivity     PipelineScopeActivity     `json:"scope_activity,omitempty"`
}

// PipelineHealth decodes the pipeline health verdict and its free-text
// reasons.
type PipelineHealth struct {
	State   string   `json:"state,omitempty"`
	Reasons []string `json:"reasons,omitempty"`
}

// PipelineQueue decodes the stack-wide reducer/ingest queue depth.
type PipelineQueue struct {
	Total                 int     `json:"total,omitempty"`
	OverdueClaims         int     `json:"overdue_claims,omitempty"`
	OldestOutstandingAgeS float64 `json:"oldest_outstanding_age,omitempty"`
	Outstanding           int     `json:"outstanding,omitempty"`
	Pending               int     `json:"pending,omitempty"`
	InFlight              int     `json:"in_flight,omitempty"`
	Retrying              int     `json:"retrying,omitempty"`
	Succeeded             int     `json:"succeeded,omitempty"`
	Failed                int     `json:"failed,omitempty"`
	DeadLetter            int     `json:"dead_letter,omitempty"`
}

// PipelineGenerationHistory decodes generation_history.
type PipelineGenerationHistory struct {
	Active     int `json:"active,omitempty"`
	Pending    int `json:"pending,omitempty"`
	Completed  int `json:"completed,omitempty"`
	Superseded int `json:"superseded,omitempty"`
	Failed     int `json:"failed,omitempty"`
	Other      int `json:"other,omitempty"`
}

// PipelineStageSummary decodes one stage_summaries entry.
type PipelineStageSummary struct {
	Stage      string `json:"stage,omitempty"`
	Pending    int    `json:"pending,omitempty"`
	Claimed    int    `json:"claimed,omitempty"`
	Running    int    `json:"running,omitempty"`
	Retrying   int    `json:"retrying,omitempty"`
	Succeeded  int    `json:"succeeded,omitempty"`
	Failed     int    `json:"failed,omitempty"`
	DeadLetter int    `json:"dead_letter,omitempty"`
}

// PipelineDomainBacklog decodes one domain_backlogs entry.
type PipelineDomainBacklog struct {
	Domain      string  `json:"domain,omitempty"`
	Outstanding int     `json:"outstanding,omitempty"`
	InFlight    int     `json:"in_flight,omitempty"`
	Blocked     int     `json:"blocked,omitempty"`
	Retrying    int     `json:"retrying,omitempty"`
	Failed      int     `json:"failed,omitempty"`
	DeadLetter  int     `json:"dead_letter,omitempty"`
	OldestAgeS  float64 `json:"oldest_age,omitempty"`
}

// PipelineScopeActivity decodes scope_activity.
type PipelineScopeActivity struct {
	Active    int `json:"active,omitempty"`
	Changed   int `json:"changed,omitempty"`
	Unchanged int `json:"unchanged,omitempty"`
}

// FetchLiveSnapshot performs the three status-route GETs and maps their
// decoded responses into an evidencebundle.LiveSnapshot. This is the only
// place in the live-export path that touches the network; evidencebundle
// itself stays a pure composer (see its README.md Ownership boundary).
//
// A non-2xx or undecodable response fails the whole export. Composing a
// bundle from a partially-fetched snapshot would publish zero counts as
// observed truth.
func FetchLiveSnapshot(fetcher StatusFetcher) (evidencebundle.LiveSnapshot, error) {
	var index IndexStatus
	if err := fetcher.Get(IndexEndpoint, &index); err != nil {
		return evidencebundle.LiveSnapshot{}, fmt.Errorf("fetch %s: %w", IndexEndpoint, err)
	}
	var pipeline PipelineStatus
	if err := fetcher.Get(PipelineEndpoint, &pipeline); err != nil {
		return evidencebundle.LiveSnapshot{}, fmt.Errorf("fetch %s: %w", PipelineEndpoint, err)
	}
	var collectors CollectorsResponse
	if err := fetcher.Get(CollectorsEndpoint, &collectors); err != nil {
		return evidencebundle.LiveSnapshot{}, fmt.Errorf("fetch %s: %w", CollectorsEndpoint, err)
	}
	return LiveSnapshotFromStatus(index, pipeline, collectors), nil
}

// LiveSnapshotFromStatus maps three decoded status responses into the
// evidencebundle.LiveSnapshot the composer consumes. It copies values through
// verbatim and composes no strings of its own, so no endpoint, target, or
// credential the caller holds can be interpolated into a bundle field here.
// The free-text values it does carry (health reasons, semantic provider
// reasons) still pass through evidencebundle.Validate's whole-document
// canaries before anything is written.
func LiveSnapshotFromStatus(
	index IndexStatus,
	pipeline PipelineStatus,
	collectors CollectorsResponse,
) evidencebundle.LiveSnapshot {
	// Every mapping below is written field by field rather than as a struct
	// conversion. The two sides are separately owned shapes: evidencebundle
	// may reorder or extend its snapshot types, and a conversion would turn
	// that into a compile break here instead of a field this package simply
	// does not carry yet.
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
		GenerationHistory: evidencebundle.LiveGenerationHistorySnapshot{
			Active:     pipeline.GenerationHistory.Active,
			Pending:    pipeline.GenerationHistory.Pending,
			Completed:  pipeline.GenerationHistory.Completed,
			Superseded: pipeline.GenerationHistory.Superseded,
			Failed:     pipeline.GenerationHistory.Failed,
			Other:      pipeline.GenerationHistory.Other,
		},
		StageSummaries: stages,
		DomainBacklogs: domains,
		Collectors:     liveCollectors,
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
func countBlockedQueueEntries(blockages []QueueBlockage) int {
	total := 0
	for _, blockage := range blockages {
		if blockage.Blocked > 0 {
			total += blockage.Blocked
		}
	}
	return total
}
