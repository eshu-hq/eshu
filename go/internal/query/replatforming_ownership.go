// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strings"
)

// Ownership-candidate kinds. A packet groups its candidates by these kinds so a
// consumer can read the likely repository, module, service, environment, and
// account for one AWS drift finding without any one signal being promoted to a
// single fabricated owner.
const (
	ownershipCandidateKindAccount     = "account"
	ownershipCandidateKindRepository  = "repository"
	ownershipCandidateKindModule      = "module"
	ownershipCandidateKindService     = "service"
	ownershipCandidateKindEnvironment = "environment"
)

// Ownership-candidate confidence labels. They are intentionally coarser than a
// numeric score: a reducer-owned candidate is at most derived, conflicting
// candidates are ambiguous, and exact is reserved for an authoritative match the
// reducer already proved (managed-by-Terraform with a matched state address).
const (
	ownershipConfidenceExact     = "exact"
	ownershipConfidenceDerived   = "derived"
	ownershipConfidenceAmbiguous = "ambiguous"
)

// Missing-evidence tokens surfaced when an attribution layer resolved nothing.
// They are stable so clients can route a follow-up collector or query call.
const (
	ownershipMissingServiceAttribution     = "service_attribution"
	ownershipMissingEnvironmentAttribution = "environment_attribution"
	ownershipMissingRepositoryAttribution  = "repository_attribution"
	ownershipMissingTerraformAddress       = "terraform_state_address"
)

// replatformingOwnershipPacket is the bounded, evidence-backed ownership view for
// one AWS drift finding. It reuses the reducer-owned candidate fields on the
// finding and the provider-neutral source-state taxonomy; it never promotes a
// raw tag, name coincidence, or single weak signal to a confident single owner.
type replatformingOwnershipPacket struct {
	// ItemID is the stable finding identity the packet drills into.
	ItemID string `json:"item_id"`
	// Provider is the cloud provider; AWS for this surface.
	Provider string `json:"provider"`
	// AccountID is the resolved AWS account, empty when unattributed.
	AccountID string `json:"account_id,omitempty"`
	// Region is the resolved AWS region, empty when unknown.
	Region string `json:"region,omitempty"`
	// ResourceType is the AWS resource family parsed from the ARN.
	ResourceType string `json:"resource_type,omitempty"`
	// StableID is the finding's stable identity (the ARN for AWS).
	StableID string `json:"stable_id,omitempty"`
	// FindingKind is the drift finding kind the packet explains.
	FindingKind string `json:"finding_kind,omitempty"`
	// ManagementStatus is the reducer-derived IaC management status.
	ManagementStatus string `json:"management_status,omitempty"`
	// SourceState is the effective provider-neutral source state after the
	// safety gate is applied; a rejected finding is never reported as ready.
	SourceState ReplatformingSourceState `json:"source_state"`
	// MatchedTerraformStateAddress is the matched Terraform state address, when
	// the reducer proved one.
	MatchedTerraformStateAddress string `json:"matched_terraform_state_address,omitempty"`
	// MatchedTerraformConfigFile is the matched Terraform config file, when
	// known.
	MatchedTerraformConfigFile string `json:"matched_terraform_config_file,omitempty"`
	// MatchedTerraformModulePath is the matched Terraform module path, when
	// known.
	MatchedTerraformModulePath string `json:"matched_terraform_module_path,omitempty"`
	// OwnerCandidates carries every candidate attribution with explicit
	// confidence and, when contested, ambiguity reasons. It is never collapsed
	// to a single guessed owner.
	OwnerCandidates []ReplatformingOwnerCandidate `json:"owner_candidates"`
	// SafetyGate is the read-only safety decision carried verbatim from the
	// finding so a consumer cannot turn a refused finding into an import.
	SafetyGate IaCManagementSafetyGate `json:"safety_gate"`
	// Freshness is the per-item freshness derived from the finding's source
	// state, so a stale or unavailable finding is visibly not fresh.
	Freshness TruthFreshness `json:"freshness"`
	// MissingEvidence lists attribution layers that resolved nothing, so a gap
	// is explicit rather than read as agreement.
	MissingEvidence []string `json:"missing_evidence,omitempty"`
	// RecommendedNextCalls lists bounded follow-up collector or query calls to
	// resolve a missing or contested attribution.
	RecommendedNextCalls []map[string]any `json:"recommended_next_calls,omitempty"`
	// Limitations carries bounded human-readable caveats for this item.
	Limitations []string `json:"limitations,omitempty"`
}

// replatformingOwnershipSummary is the bounded set of ownership packets for one
// page of findings plus the explicit ambiguous and unattributed counts an
// operator reads before promoting any owner.
type replatformingOwnershipSummary struct {
	Packets           []replatformingOwnershipPacket
	AmbiguousCount    int
	UnattributedCount int
	RejectedCount     int
}

// buildReplatformingOwnershipSummary composes one ownership packet per finding
// and counts the contested and unattributed items. Each finding is processed
// independently and deterministically; the summary never reorders findings.
func buildReplatformingOwnershipSummary(
	findings []IaCManagementFindingRow,
	filter IaCManagementFilter,
) replatformingOwnershipSummary {
	summary := replatformingOwnershipSummary{
		Packets: make([]replatformingOwnershipPacket, 0, len(findings)),
	}
	for i := range findings {
		packet := buildReplatformingOwnershipPacket(findings[i], filter)
		summary.Packets = append(summary.Packets, packet)
		if packet.SourceState == ReplatformingSourceStateRejected {
			summary.RejectedCount++
		}
		if ownershipPacketIsAmbiguous(packet) {
			summary.AmbiguousCount++
		}
		if ownershipPacketIsUnattributed(packet) {
			summary.UnattributedCount++
		}
	}
	return summary
}

// buildReplatformingOwnershipPacket composes the ownership packet for one
// finding. It resolves the effective source state through the safety gate,
// builds owner/repo/module/service/environment candidates from reducer-owned
// fields only, and records every attribution gap as explicit missing evidence
// with a bounded follow-up call. Raw tags stay provenance-only and never become
// owner candidates.
func buildReplatformingOwnershipPacket(
	finding IaCManagementFindingRow,
	filter IaCManagementFilter,
) replatformingOwnershipPacket {
	normalizeIaCManagementFindingSafety(&finding)
	state := ResolveReplatformingSourceState(finding.ManagementStatus, finding.SafetyGate.ReviewRequired)

	packet := replatformingOwnershipPacket{
		ItemID:                       finding.ID,
		Provider:                     iacFirstNonEmpty(finding.Provider, "aws"),
		AccountID:                    ownershipAccountID(finding, filter),
		Region:                       strings.TrimSpace(finding.Region),
		ResourceType:                 strings.TrimSpace(finding.ResourceType),
		StableID:                     strings.TrimSpace(finding.ARN),
		FindingKind:                  strings.TrimSpace(finding.FindingKind),
		ManagementStatus:             strings.TrimSpace(finding.ManagementStatus),
		SourceState:                  state,
		MatchedTerraformStateAddress: strings.TrimSpace(finding.MatchedTerraformStateAddress),
		MatchedTerraformConfigFile:   strings.TrimSpace(finding.MatchedTerraformConfigFile),
		MatchedTerraformModulePath:   strings.TrimSpace(finding.MatchedTerraformModulePath),
		SafetyGate:                   finding.SafetyGate,
		Freshness:                    ownershipFreshnessForState(state),
		Limitations:                  ownershipPacketLimitations(),
	}

	var missing []string
	packet.OwnerCandidates, missing = ownershipCandidates(finding, packet.AccountID)
	packet.MissingEvidence = missing
	packet.RecommendedNextCalls = ownershipRecommendedNextCalls(finding, missing)
	return packet
}

// ownershipCandidates assembles the candidate list and the missing-evidence
// tokens. It groups candidates by kind, labels confidence, and folds every
// missing attribution layer into the returned slice. Account is the only
// candidate kind that can be exact, and only when an exact account is resolved.
func ownershipCandidates(
	finding IaCManagementFindingRow,
	accountID string,
) ([]ReplatformingOwnerCandidate, []string) {
	var candidates []ReplatformingOwnerCandidate
	var missing []string

	if accountID != "" {
		candidates = append(candidates, ReplatformingOwnerCandidate{
			Kind:       ownershipCandidateKindAccount,
			Value:      accountID,
			Confidence: ownershipConfidenceDerived,
		})
	}

	// Terraform state/config/module describe the proven IaC destination, not the
	// runtime owner; a matched state address is the only exact ownership signal
	// here because the reducer correlated it directly.
	if module := strings.TrimSpace(finding.MatchedTerraformModulePath); module != "" {
		confidence := ownershipConfidenceDerived
		if strings.TrimSpace(finding.MatchedTerraformStateAddress) != "" {
			confidence = ownershipConfidenceExact
		}
		candidates = append(candidates, ReplatformingOwnerCandidate{
			Kind:       ownershipCandidateKindModule,
			Value:      module,
			Confidence: confidence,
		})
	}

	serviceCandidates, serviceMissing := ownershipKindCandidates(
		ownershipCandidateKindService,
		finding.ServiceCandidates,
		ownershipMissingServiceAttribution,
	)
	candidates = append(candidates, serviceCandidates...)
	missing = append(missing, serviceMissing...)

	environmentCandidates, environmentMissing := ownershipKindCandidates(
		ownershipCandidateKindEnvironment,
		finding.EnvironmentCandidates,
		ownershipMissingEnvironmentAttribution,
	)
	candidates = append(candidates, environmentCandidates...)
	missing = append(missing, environmentMissing...)

	repositoryCandidates, repositoryMissing := ownershipRepositoryCandidates(finding)
	candidates = append(candidates, repositoryCandidates...)
	missing = append(missing, repositoryMissing...)

	if strings.TrimSpace(finding.MatchedTerraformStateAddress) == "" {
		missing = append(missing, ownershipMissingTerraformAddress)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Kind != candidates[j].Kind {
			return candidates[i].Kind < candidates[j].Kind
		}
		return candidates[i].Value < candidates[j].Value
	})
	return candidates, dedupeSortedStrings(missing)
}

// ownershipKindCandidates turns a reducer-owned candidate slice for one kind
// into ownership candidates. Zero candidates yield no candidate and the missing
// token. A single distinct candidate is derived (never exact). Two or more
// distinct candidates are each marked ambiguous and carry the conflicting set as
// ambiguity reasons, so a contested attribution is never collapsed to one owner.
func ownershipKindCandidates(
	kind string,
	rawCandidates []string,
	missingToken string,
) ([]ReplatformingOwnerCandidate, []string) {
	distinct := dedupeNonEmpty(rawCandidates)
	switch len(distinct) {
	case 0:
		return nil, []string{missingToken}
	case 1:
		return []ReplatformingOwnerCandidate{{
			Kind:       kind,
			Value:      distinct[0],
			Confidence: ownershipConfidenceDerived,
		}}, nil
	default:
		reasons := ownershipAmbiguityReasons(kind, distinct)
		out := make([]ReplatformingOwnerCandidate, 0, len(distinct))
		for _, value := range distinct {
			out = append(out, ReplatformingOwnerCandidate{
				Kind:             kind,
				Value:            value,
				Confidence:       ownershipConfidenceAmbiguous,
				AmbiguityReasons: reasons,
			})
		}
		return out, nil
	}
}

// ownershipRepositoryCandidates derives repository candidates from the matched
// Terraform config file path only. A config file lives in a source repository,
// so its repo prefix is a derived repository hint, never an exact owner. Without
// a config file path there is no reducer-owned repository signal, so the packet
// records the gap instead of guessing from a tag or name.
func ownershipRepositoryCandidates(finding IaCManagementFindingRow) ([]ReplatformingOwnerCandidate, []string) {
	configFile := strings.TrimSpace(finding.MatchedTerraformConfigFile)
	if configFile == "" {
		return nil, []string{ownershipMissingRepositoryAttribution}
	}
	repo := ownershipRepositoryHintFromConfig(configFile)
	if repo == "" {
		return nil, []string{ownershipMissingRepositoryAttribution}
	}
	return []ReplatformingOwnerCandidate{{
		Kind:       ownershipCandidateKindRepository,
		Value:      repo,
		Confidence: ownershipConfidenceDerived,
		AmbiguityReasons: []string{
			"repository inferred from matched Terraform config path; confirm against repository ownership before promoting",
		},
	}}, nil
}

// ownershipRepositoryHintFromConfig returns the leading path segment of a
// Terraform config file path as a bounded repository hint. A bare filename has
// no directory prefix and yields no hint.
func ownershipRepositoryHintFromConfig(configFile string) string {
	cleaned := strings.TrimLeft(strings.TrimSpace(configFile), "./")
	if idx := strings.IndexByte(cleaned, '/'); idx > 0 {
		return cleaned[:idx]
	}
	return ""
}

// ownershipAmbiguityReasons builds the explicit reason a contested candidate set
// is ambiguous, listing the conflicting values in deterministic order.
func ownershipAmbiguityReasons(kind string, distinct []string) []string {
	return []string{
		"multiple deterministic " + kind + " candidates conflict (" +
			strings.Join(distinct, ", ") +
			"); resolve before promoting a single owner",
	}
}

// ownershipAccountID resolves the finding's account, falling back to the filter
// account then the scope account. It never fabricates an account.
func ownershipAccountID(finding IaCManagementFindingRow, filter IaCManagementFilter) string {
	if id := strings.TrimSpace(finding.AccountID); id != "" {
		return id
	}
	if id := strings.TrimSpace(filter.AccountID); id != "" {
		return id
	}
	return terraformImportScopeParts(filter.ScopeID).accountID
}

// ownershipFreshnessForState maps the effective source state to a per-item
// freshness state so a stale or unavailable finding is never reported as fresh.
func ownershipFreshnessForState(state ReplatformingSourceState) TruthFreshness {
	switch state {
	case ReplatformingSourceStateStale:
		return TruthFreshness{State: FreshnessStale}
	case ReplatformingSourceStateUnavailable, ReplatformingSourceStateUnknown:
		return TruthFreshness{State: FreshnessUnavailable}
	default:
		return TruthFreshness{State: FreshnessFresh}
	}
}

// ownershipRecommendedNextCalls returns bounded follow-up calls for the gaps a
// packet recorded, so a consumer knows where to drill before promoting an owner.
func ownershipRecommendedNextCalls(finding IaCManagementFindingRow, missing []string) []map[string]any {
	var calls []map[string]any
	if containsString(missing, ownershipMissingServiceAttribution) ||
		containsString(missing, ownershipMissingEnvironmentAttribution) {
		calls = append(calls, map[string]any{
			"tool":   "explain_iac_management_status",
			"route":  "POST /api/v0/iac/management-status/explain",
			"reason": "inspect grouped evidence for the finding before attributing a service or environment",
		})
	}
	if containsString(missing, ownershipMissingTerraformAddress) {
		calls = append(calls, map[string]any{
			"tool":   "find_unmanaged_resources",
			"route":  "POST /api/v0/iac/unmanaged-resources",
			"reason": "confirm whether a Terraform state address exists for this resource before import planning",
		})
	}
	return calls
}

// ownershipPacketLimitations are the fixed per-item caveats every ownership
// packet repeats so a consumer cannot mistake candidates for confirmed owners.
func ownershipPacketLimitations() []string {
	return []string{
		"owner, repository, module, service, and environment values are candidates, not confirmed ownership",
		"raw tags remain provenance evidence and never become owner candidates",
		"a single candidate is derived, never exact; conflicting candidates carry explicit ambiguity reasons",
	}
}

// ownershipPacketIsAmbiguous reports whether any candidate on the packet is
// contested, so the summary can count contested items without re-deriving.
func ownershipPacketIsAmbiguous(packet replatformingOwnershipPacket) bool {
	for _, candidate := range packet.OwnerCandidates {
		if candidate.Confidence == ownershipConfidenceAmbiguous {
			return true
		}
	}
	return packet.SourceState == ReplatformingSourceStateAmbiguous
}

// ownershipPacketIsUnattributed reports whether the packet resolved neither a
// service nor an environment candidate, so the summary surfaces missing
// attribution rather than folding it into an owner.
func ownershipPacketIsUnattributed(packet replatformingOwnershipPacket) bool {
	return containsString(packet.MissingEvidence, ownershipMissingServiceAttribution) &&
		containsString(packet.MissingEvidence, ownershipMissingEnvironmentAttribution)
}

// dedupeNonEmpty trims, de-duplicates, and sorts a candidate slice so candidate
// order is deterministic and blank values never become a candidate.
func dedupeNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// dedupeSortedStrings trims, de-duplicates, and sorts the missing-evidence
// tokens so the response is deterministic and free of blanks.
func dedupeSortedStrings(values []string) []string {
	out := dedupeNonEmpty(values)
	if len(out) == 0 {
		return nil
	}
	return out
}
