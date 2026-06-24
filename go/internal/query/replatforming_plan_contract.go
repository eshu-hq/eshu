// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

// ReplatformingPlanContractVersion is the wire version of the replatforming
// plan / migration packet contract. Clients must pin to it; an incompatible
// shape change bumps this value.
const ReplatformingPlanContractVersion = "v1"

// ReplatformingScopeKind names the dimension a replatforming plan was requested
// for. A plan is always anchored on exactly one primary scope kind even when it
// carries additional narrowing fields.
type ReplatformingScopeKind string

const (
	// ReplatformingScopeAccount anchors a plan on one cloud account.
	ReplatformingScopeAccount ReplatformingScopeKind = "account"
	// ReplatformingScopeRegion anchors a plan on one account region.
	ReplatformingScopeRegion ReplatformingScopeKind = "region"
	// ReplatformingScopeService anchors a plan on one service.
	ReplatformingScopeService ReplatformingScopeKind = "service"
	// ReplatformingScopeWorkload anchors a plan on one deployable workload.
	ReplatformingScopeWorkload ReplatformingScopeKind = "workload"
	// ReplatformingScopeRepository anchors a plan on one source repository.
	ReplatformingScopeRepository ReplatformingScopeKind = "repository"
	// ReplatformingScopeEnvironment anchors a plan on one environment.
	ReplatformingScopeEnvironment ReplatformingScopeKind = "environment"
	// ReplatformingScopeResource anchors a plan on one stable resource identity.
	ReplatformingScopeResource ReplatformingScopeKind = "resource"
)

// ReplatformingPlanScope is the requested scope of a replatforming plan. Kind
// names the primary dimension; the remaining fields narrow it and stay empty
// when they do not apply.
type ReplatformingPlanScope struct {
	Kind        ReplatformingScopeKind `json:"kind"`
	Account     string                 `json:"account,omitempty"`
	Region      string                 `json:"region,omitempty"`
	Service     string                 `json:"service,omitempty"`
	Workload    string                 `json:"workload,omitempty"`
	Repository  string                 `json:"repository,omitempty"`
	Environment string                 `json:"environment,omitempty"`
	Resource    string                 `json:"resource,omitempty"`
}

// ReplatformingSourceLayer names one evidence layer that a migration packet item
// can have or be missing.
type ReplatformingSourceLayer string

const (
	// ReplatformingSourceDeclaredIaC is source-controlled IaC / Terraform
	// configuration.
	ReplatformingSourceDeclaredIaC ReplatformingSourceLayer = "declared_iac"
	// ReplatformingSourceAppliedState is applied Terraform state.
	ReplatformingSourceAppliedState ReplatformingSourceLayer = "applied_terraform_state"
	// ReplatformingSourceObservedRuntime is observed provider runtime evidence.
	ReplatformingSourceObservedRuntime ReplatformingSourceLayer = "observed_provider_runtime"
	// ReplatformingSourceMissingEvidence records an explicitly absent layer so a
	// gap is never read as agreement.
	ReplatformingSourceMissingEvidence ReplatformingSourceLayer = "missing_evidence"
)

// ReplatformingSourceLayerStatus reports the per-layer source state for one
// migration packet item using the provider-neutral source-state taxonomy.
type ReplatformingSourceLayerStatus struct {
	Layer  ReplatformingSourceLayer `json:"layer"`
	State  ReplatformingSourceState `json:"state"`
	Detail string                   `json:"detail,omitempty"`
}

// ReplatformingOwnerCandidate is one possible owner, repo, module, account,
// owner, or environment attribution for a packet item. Multiple candidates of
// the same kind must carry explicit ambiguity reasons; ownership is never
// promoted to a single fabricated owner.
type ReplatformingOwnerCandidate struct {
	Kind             string   `json:"kind"`
	Value            string   `json:"value"`
	Confidence       string   `json:"confidence,omitempty"`
	AmbiguityReasons []string `json:"ambiguity_reasons,omitempty"`
}

// ReplatformingImportCandidate is the read-only Terraform import candidate for a
// packet item. A refused candidate carries refusal reasons and never an import
// block.
type ReplatformingImportCandidate struct {
	Status         string   `json:"status"`
	ResourceType   string   `json:"resource_type,omitempty"`
	ImportBlock    string   `json:"import_block,omitempty"`
	RefusalReasons []string `json:"refusal_reasons,omitempty"`
}

const (
	// ReplatformingImportStatusReady marks a candidate with a ready import block.
	ReplatformingImportStatusReady = "ready"
	// ReplatformingImportStatusRefused marks a candidate refused with reasons.
	ReplatformingImportStatusRefused = "refused"
)

// MigrationPacketItem is one candidate resource in a replatforming plan with its
// management status, finding kind, confidence, freshness, safety gate, and
// provider-neutral source state.
type MigrationPacketItem struct {
	ItemID           string                           `json:"item_id"`
	Provider         string                           `json:"provider"`
	ResourceType     string                           `json:"resource_type"`
	StableID         string                           `json:"stable_id"`
	SourceState      ReplatformingSourceState         `json:"source_state"`
	ManagementStatus string                           `json:"management_status,omitempty"`
	FindingKind      string                           `json:"finding_kind,omitempty"`
	Confidence       string                           `json:"confidence,omitempty"`
	Freshness        *TruthFreshness                  `json:"freshness,omitempty"`
	SafetyGate       IaCManagementSafetyGate          `json:"safety_gate"`
	SourceLayers     []ReplatformingSourceLayerStatus `json:"source_layers,omitempty"`
	OwnerCandidates  []ReplatformingOwnerCandidate    `json:"owner_candidates,omitempty"`
	ImportCandidate  *ReplatformingImportCandidate    `json:"import_candidate,omitempty"`
	WaveID           string                           `json:"wave_id,omitempty"`
	BlastRadiusGroup string                           `json:"blast_radius_group,omitempty"`
}

// MigrationWave is one ordered group of packet items for staged migration.
type MigrationWave struct {
	ID        string   `json:"id"`
	Order     int      `json:"order"`
	Rationale string   `json:"rationale,omitempty"`
	ItemIDs   []string `json:"item_ids,omitempty"`
}

// BlastRadiusGroup groups packet items by dependency and risk so that a wave's
// impact is explicit before any external apply.
type BlastRadiusGroup struct {
	ID       string   `json:"id"`
	Severity string   `json:"severity,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	ItemIDs  []string `json:"item_ids,omitempty"`
}

// ReplatformingPlan is the versioned replatforming plan / migration packet. It
// preserves truth labels and per-item source state, and it always names its
// non-goals so a consumer cannot mistake it for an execution surface.
type ReplatformingPlan struct {
	ContractVersion      string                 `json:"contract_version"`
	Scope                ReplatformingPlanScope `json:"scope"`
	Items                []MigrationPacketItem  `json:"items"`
	Waves                []MigrationWave        `json:"waves,omitempty"`
	BlastRadiusGroups    []BlastRadiusGroup     `json:"blast_radius_groups,omitempty"`
	RecommendedNextCalls []map[string]any       `json:"recommended_next_calls,omitempty"`
	Limitations          []string               `json:"limitations,omitempty"`
	NonGoals             []string               `json:"non_goals"`
}

// ReplatformingPlanNonGoals are the fixed safety boundaries every plan repeats.
// They are part of the contract: Eshu observes, compares, and plans, but never
// executes a migration.
func ReplatformingPlanNonGoals() []string {
	return []string{
		"does not run Terraform or any migration",
		"does not import resources or mutate cloud state",
		"does not write user repositories",
		"does not promote provider observations to ownership truth without reducer-owned evidence",
	}
}

// NewReplatformingPlan returns an empty plan pinned to the current contract
// version and pre-populated with the fixed non-goals.
func NewReplatformingPlan(scope ReplatformingPlanScope) ReplatformingPlan {
	return ReplatformingPlan{
		ContractVersion: ReplatformingPlanContractVersion,
		Scope:           scope,
		NonGoals:        ReplatformingPlanNonGoals(),
	}
}

// TruthLevel maps a source state to the authoritative truth level it may carry.
// Only exact evidence is authoritative; deterministic-but-partial evidence is
// derived; every uncertain, unavailable, or gated state is fallback so it can
// never present as authoritative truth.
func (s ReplatformingSourceState) TruthLevel() TruthLevel {
	switch s {
	case ReplatformingSourceStateExact:
		return TruthLevelExact
	case ReplatformingSourceStateDerived, ReplatformingSourceStatePartial:
		return TruthLevelDerived
	default:
		return TruthLevelFallback
	}
}

// RollupTruthLevel returns the most conservative truth level across all items.
// An empty plan is a valid bounded answer at derived. The rollup never claims
// exact unless every item is exact, so a plan cannot over-state authority.
func (p *ReplatformingPlan) RollupTruthLevel() TruthLevel {
	if p == nil || len(p.Items) == 0 {
		return TruthLevelDerived
	}
	rollup := TruthLevelExact
	for _, item := range p.Items {
		rollup = minTruthLevel(rollup, item.SourceState.TruthLevel())
	}
	return rollup
}

// normalizedOwnerKind lowercases and trims an owner-candidate kind for grouping.
func normalizedOwnerKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}
