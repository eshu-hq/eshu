// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencebundle

import (
	"strings"
	"time"
)

// liveProfile is deliberately "unknown". Nothing in the three status routes
// reports which runtime profile the target stack runs, so stamping
// "live_authoritative" would assign authority the source may not have — a
// lightweight local stack and production would look identical. The gap is
// recorded in Missing rather than guessed.
const liveProfile = "unknown"

// LiveSnapshot is the caller-fetched status evidence used to compose a live
// evidence bundle. This package never dials a status endpoint itself (see
// README.md Ownership boundary); callers such as cmd/eshu fetch
// GET /api/v0/status/index, GET /api/v0/status/pipeline, and
// GET /api/v0/status/collectors, decode them, and populate this struct
// before calling BuildLiveBundle. Every field carries only a derived,
// non-locating value: no host, port, or endpoint URL belongs here. Free-text
// fields (HealthReasons, SemanticExtraction.Reason) still pass through
// Validate's private-data canaries as defense in depth, since status text can
// echo operator-authored strings.
type LiveSnapshot struct {
	RepositoryCount    int
	HealthState        string
	HealthReasons      []string
	Queue              LiveQueueSnapshot
	QueueBlockedCount  int
	ScopeActivity      LiveScopeActivitySnapshot
	GenerationHistory  LiveGenerationHistorySnapshot
	StageSummaries     []LiveStageSummarySnapshot
	DomainBacklogs     []LiveDomainBacklogSnapshot
	Collectors         []LiveCollectorSnapshot
	SemanticExtraction LiveSemanticExtractionSnapshot
}

// LiveQueueSnapshot mirrors the reducer/ingest queue depth reported by
// GET /api/v0/status/pipeline.
type LiveQueueSnapshot struct {
	Total                 int
	Outstanding           int
	OverdueClaims         int
	OldestOutstandingAgeS float64
	Pending               int
	InFlight              int
	Retrying              int
	Succeeded             int
	Failed                int
	DeadLetter            int
}

// LiveScopeActivitySnapshot mirrors scope_activity from the pipeline status
// endpoint.
type LiveScopeActivitySnapshot struct {
	Active    int
	Changed   int
	Unchanged int
}

// LiveGenerationHistorySnapshot mirrors generation_history from the pipeline
// status endpoint.
type LiveGenerationHistorySnapshot struct {
	Active     int
	Pending    int
	Completed  int
	Superseded int
	Failed     int
	Other      int
}

// LiveStageSummarySnapshot mirrors one stage_summaries entry from the
// pipeline status endpoint.
type LiveStageSummarySnapshot struct {
	Stage      string
	Pending    int
	Claimed    int
	Running    int
	Retrying   int
	Succeeded  int
	Failed     int
	DeadLetter int
}

// LiveDomainBacklogSnapshot mirrors one domain_backlogs entry from the
// pipeline status endpoint.
type LiveDomainBacklogSnapshot struct {
	Domain      string
	Outstanding int
	InFlight    int
	Retrying    int
	Failed      int
	DeadLetter  int
}

// LiveCollectorSnapshot mirrors one collector entry from
// GET /api/v0/status/collectors.
type LiveCollectorSnapshot struct {
	CollectorKind  string
	StatusCategory string
	Health         string
}

// LiveSemanticExtractionSnapshot mirrors semantic_extraction from
// GET /api/v0/status/index.
type LiveSemanticExtractionSnapshot struct {
	State              string
	Reason             string
	ProviderConfigured bool
	ProviderProfiles   []LiveSemanticProviderProfileSnapshot
}

// LiveSemanticProviderProfileSnapshot mirrors one provider_profiles entry
// from the semantic_extraction status block.
type LiveSemanticProviderProfileSnapshot struct {
	ProfileID    string
	ProviderKind string
	State        string
	Reason       string
}

// LiveBundleOptions controls live bundle identity fields that are not
// sourced from the fetched LiveSnapshot itself.
type LiveBundleOptions struct {
	// ScopeID names the bounded bundle scope. Defaults to "live:local" when
	// empty.
	ScopeID string
	// CreatedAt stamps Identity.CreatedAt. BuildLiveBundle is a pure function
	// and never calls time.Now itself, so callers must supply the fetch time.
	CreatedAt time.Time
}

// BuildLiveBundle composes a share-safe evidence_bundle.v1 snapshot from an
// already-fetched LiveSnapshot. It is a pure function: it performs no
// network calls, matching the evidencebundle package's ownership boundary
// (see README.md). Callers MUST still run Validate on the result before
// treating it as share-safe, exactly as the demo path does.
func BuildLiveBundle(snapshot LiveSnapshot, opts LiveBundleOptions) Bundle {
	scopeID := strings.TrimSpace(opts.ScopeID)
	if scopeID == "" {
		scopeID = "live:local"
	}
	createdAt := opts.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Unix(0, 0).UTC()
	}

	pipelineState := buildPipelineStateSnapshot(snapshot)
	semanticState := buildSemanticProviderStateSnapshot(snapshot.SemanticExtraction)

	bundle := Bundle{
		SchemaVersion: SchemaVersion,
		Identity: Identity{
			ScopeID:   scopeID,
			Profile:   liveProfile,
			CreatedAt: createdAt.UTC().Format(time.RFC3339),
		},
		Source: SourceIdentity{
			Repository: "live:stack",
		},
		Redaction: RedactionProfile{
			Profile: "share_safe_v1",
			Rules: []string{
				"handles_only",
				"screened_private_endpoints",
				"screened_credentials",
				"screened_model_inputs_or_outputs",
			},
		},
		Contents: Contents{
			OperatorState: []OperatorStateItem{
				{Kind: "freshness", State: "unknown", Reason: "no_freshness_signal_in_status_routes"},
				{Kind: "readiness", State: liveReadinessState(snapshot.HealthState)},
			},
			PipelineState:         &pipelineState,
			SemanticProviderState: &semanticState,
		},
		Missing: []MissingEvidence{
			{
				Family: "fact_counts",
				Reason: "fact_counts_not_exposed_by_status_api",
			},
			{
				Family: "freshness",
				Reason: "status_routes_carry_no_indexed_at_or_generation_completed_at",
			},
			{
				Family: "runtime_profile",
				Reason: "status_routes_do_not_report_the_target_runtime_profile",
			},
		},

		Reproduce: []ReproduceCall{
			{Kind: "cli", Target: "eshu evidence bundle export --live"},
			{Kind: "api", Target: "GET /api/v0/status/index"},
			{Kind: "api", Target: "GET /api/v0/status/pipeline"},
			{Kind: "api", Target: "GET /api/v0/status/collectors"},
		},
		Bounds: Bounds{
			MaxAnswerPackets:        25,
			MaxInvestigationPackets: 25,
			MaxHandles:              200,
		},
		Validation: Validation{
			// Built unvalidated on purpose: a builder that stamps "passed"
			// certifies a check it never ran. StampValidation applies "passed"
			// after Validate returns nil.
			Status: unvalidatedStatus,
			Checks: []string{
				"schema",
				"redaction",
				"private_data_canaries",
				"reproduce_handles",
			},
		},
	}
	// A zero repository count cannot be distinguished from an unavailable one:
	// the index handler initialises the count to zero and suppresses a failed
	// graph query, returning 200 either way. Recording the gap keeps a
	// lightweight or degraded stack from reading as a genuinely empty one.
	if snapshot.RepositoryCount == 0 {
		bundle.Missing = append(bundle.Missing, MissingEvidence{
			Family: "repository_count",
			Reason: "zero_is_ambiguous_between_empty_graph_and_unavailable_graph_count",
		})
	}

	sortBundle(&bundle)
	bundle.BundleID = bundleID(bundle)
	return bundle
}

func buildPipelineStateSnapshot(snapshot LiveSnapshot) PipelineStateSnapshot {
	stages := make([]PipelineStageSummarySnapshot, 0, len(snapshot.StageSummaries))
	for _, stage := range snapshot.StageSummaries {
		stages = append(stages, PipelineStageSummarySnapshot(stage))
	}
	domains := make([]PipelineDomainBacklogSnapshot, 0, len(snapshot.DomainBacklogs))
	for _, domain := range snapshot.DomainBacklogs {
		domains = append(domains, PipelineDomainBacklogSnapshot(domain))
	}
	collectors := make([]PipelineCollectorReadinessSnapshot, 0, len(snapshot.Collectors))
	for _, collector := range snapshot.Collectors {
		collectors = append(collectors, PipelineCollectorReadinessSnapshot(collector))
	}
	return PipelineStateSnapshot{
		RepositoryCount:   snapshot.RepositoryCount,
		HealthState:       snapshot.HealthState,
		HealthReasons:     append([]string(nil), snapshot.HealthReasons...),
		Queue:             PipelineQueueSnapshot(snapshot.Queue),
		QueueBlockedCount: snapshot.QueueBlockedCount,
		ScopeActivity: PipelineScopeActivitySnapshot{
			Active:    snapshot.ScopeActivity.Active,
			Changed:   snapshot.ScopeActivity.Changed,
			Unchanged: snapshot.ScopeActivity.Unchanged,
		},
		GenerationHistory: PipelineGenerationHistorySnapshot(snapshot.GenerationHistory),
		StageSummaries:    stages,
		DomainBacklogs:    domains,
		Collectors:        collectors,
	}
}

func buildSemanticProviderStateSnapshot(snapshot LiveSemanticExtractionSnapshot) SemanticProviderStateSnapshot {
	profiles := make([]SemanticProviderProfileSnapshot, 0, len(snapshot.ProviderProfiles))
	for _, profile := range snapshot.ProviderProfiles {
		profiles = append(profiles, SemanticProviderProfileSnapshot(profile))
	}
	return SemanticProviderStateSnapshot{
		State:              snapshot.State,
		Reason:             snapshot.Reason,
		ProviderConfigured: snapshot.ProviderConfigured,
		ProviderProfiles:   profiles,
	}
}

func liveReadinessState(healthState string) string {
	switch strings.ToLower(strings.TrimSpace(healthState)) {
	case "healthy":
		return "ready"
	case "degraded":
		return "ready_with_findings"
	case "progressing":
		// The normal state while a stack is still indexing. Folding it into the
		// default bucket made a healthy first-run capture read as an
		// unrecognised value.
		return "indexing"
	case "stalled":
		return "blocked"
	case "":
		return "unknown"
	default:
		return "unknown"
	}
}
