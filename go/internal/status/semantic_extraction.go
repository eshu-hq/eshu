package status

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	// SemanticExtractionUnavailable means no semantic extraction provider can run.
	SemanticExtractionUnavailable = "unavailable"
	// SemanticExtractionAvailable means provider-backed semantic extraction can run.
	SemanticExtractionAvailable = "available"
	// SemanticExtractionAvailableButDisabledForScope means the provider is
	// configured, but the selected scope is disabled.
	SemanticExtractionAvailableButDisabledForScope = "available_but_disabled_for_scope"
	// SemanticExtractionDisabledByPolicy means policy disables semantic extraction.
	SemanticExtractionDisabledByPolicy = "disabled_by_policy"
	// SemanticExtractionProviderUnhealthy means a configured provider is not healthy.
	SemanticExtractionProviderUnhealthy = "provider_unhealthy"
)

const (
	// SemanticExtractionReasonProviderNotConfigured is the stable no-provider reason.
	SemanticExtractionReasonProviderNotConfigured = "provider_not_configured"
	// SemanticExtractionReasonProviderConfigured marks an available provider.
	SemanticExtractionReasonProviderConfigured = "provider_configured"
	// SemanticExtractionReasonScopeDisabled marks a scope-level disablement.
	SemanticExtractionReasonScopeDisabled = "scope_disabled"
	// SemanticExtractionReasonPolicyDisabled marks an operator policy disablement.
	SemanticExtractionReasonPolicyDisabled = "policy_disabled"
	// SemanticExtractionReasonProviderUnhealthy marks a configured but unhealthy provider.
	SemanticExtractionReasonProviderUnhealthy = "provider_unhealthy"
	// SemanticExtractionReasonInvalidState marks a malformed status row.
	SemanticExtractionReasonInvalidState = "invalid_semantic_extraction_state"
)

var semanticExtractionStates = []string{
	SemanticExtractionUnavailable,
	SemanticExtractionAvailable,
	SemanticExtractionAvailableButDisabledForScope,
	SemanticExtractionDisabledByPolicy,
	SemanticExtractionProviderUnhealthy,
}

// SemanticExtractionStatus captures optional LLM-assisted extraction liveness.
type SemanticExtractionStatus struct {
	State                            string
	Reason                           string
	Detail                           string
	ProviderConfigured               bool
	DocumentationObservationsEnabled bool
	CodeHintsEnabled                 bool
	DeterministicPathsAffected       bool
	UpdatedAt                        time.Time
	ProviderProfiles                 []SemanticProviderProfileStatus
}

// SemanticExtractionSupportedStates returns the stable status enum values.
func SemanticExtractionSupportedStates() []string {
	return slices.Clone(semanticExtractionStates)
}

// DefaultSemanticExtractionStatus returns the zero-key runtime status.
func DefaultSemanticExtractionStatus() SemanticExtractionStatus {
	return SemanticExtractionStatus{
		State:                            SemanticExtractionUnavailable,
		Reason:                           SemanticExtractionReasonProviderNotConfigured,
		Detail:                           "no semantic extraction provider is configured; deterministic indexing, reducer projection, API reads, MCP tools, and documentation fact verification are unaffected",
		ProviderConfigured:               false,
		DocumentationObservationsEnabled: false,
		CodeHintsEnabled:                 false,
		DeterministicPathsAffected:       false,
	}
}

func normalizeSemanticExtractionStatus(snapshot SemanticExtractionStatus) SemanticExtractionStatus {
	profiles := cloneSemanticProviderProfiles(snapshot.ProviderProfiles)
	state := strings.TrimSpace(snapshot.State)
	if state == "" {
		if len(profiles) > 0 {
			return semanticExtractionStatusFromProviderProfiles(snapshot, profiles)
		}
		return DefaultSemanticExtractionStatus()
	}
	if !isSemanticExtractionState(state) {
		out := DefaultSemanticExtractionStatus()
		out.Reason = SemanticExtractionReasonInvalidState
		out.Detail = fmt.Sprintf("semantic extraction status %q is unsupported; treating semantic extraction as unavailable", state)
		out.UpdatedAt = snapshot.UpdatedAt
		out.ProviderProfiles = profiles
		return out
	}

	out := SemanticExtractionStatus{
		State:                            state,
		Reason:                           strings.TrimSpace(snapshot.Reason),
		Detail:                           strings.TrimSpace(snapshot.Detail),
		ProviderConfigured:               snapshot.ProviderConfigured,
		DocumentationObservationsEnabled: snapshot.DocumentationObservationsEnabled,
		CodeHintsEnabled:                 snapshot.CodeHintsEnabled,
		DeterministicPathsAffected:       false,
		UpdatedAt:                        snapshot.UpdatedAt,
		ProviderProfiles:                 profiles,
	}
	if len(profiles) > 0 {
		out.ProviderConfigured = out.ProviderConfigured || semanticProfilesConfigured(profiles)
		out.DocumentationObservationsEnabled = out.DocumentationObservationsEnabled ||
			semanticProfilesAllowSource(profiles, "documentation")
		out.CodeHintsEnabled = out.CodeHintsEnabled || semanticProfilesAllowSource(profiles, "code_hints")
	}
	if out.Reason == "" {
		out.Reason = defaultSemanticExtractionReason(out.State)
	}
	if out.Detail == "" {
		out.Detail = defaultSemanticExtractionDetail(out.State)
	}
	switch out.State {
	case SemanticExtractionAvailable, SemanticExtractionAvailableButDisabledForScope, SemanticExtractionProviderUnhealthy:
		out.ProviderConfigured = true
	case SemanticExtractionUnavailable:
		out.ProviderConfigured = false
	}
	if out.State != SemanticExtractionAvailable {
		out.DocumentationObservationsEnabled = false
		out.CodeHintsEnabled = false
	}
	return out
}

func semanticExtractionStatusFromProviderProfiles(
	snapshot SemanticExtractionStatus,
	profiles []SemanticProviderProfileStatus,
) SemanticExtractionStatus {
	out := SemanticExtractionStatus{
		UpdatedAt:        snapshot.UpdatedAt,
		ProviderProfiles: profiles,
	}
	out.ProviderConfigured = semanticProfilesConfigured(profiles)
	out.DocumentationObservationsEnabled = semanticProfilesAllowSource(profiles, "documentation")
	out.CodeHintsEnabled = semanticProfilesAllowSource(profiles, "code_hints")

	switch {
	case semanticProfilesUnhealthy(profiles):
		out.State = SemanticExtractionProviderUnhealthy
		out.Reason = SemanticExtractionReasonProviderUnhealthy
	case !out.ProviderConfigured:
		out.State = SemanticExtractionUnavailable
		out.Reason = SemanticExtractionReasonProviderNotConfigured
	case !semanticProfilesHaveAnySourcePolicy(profiles):
		out.State = SemanticExtractionDisabledByPolicy
		out.Reason = SemanticExtractionReasonPolicyDisabled
	case out.DocumentationObservationsEnabled || out.CodeHintsEnabled:
		out.State = SemanticExtractionAvailable
		out.Reason = SemanticExtractionReasonProviderConfigured
	default:
		out.State = SemanticExtractionAvailableButDisabledForScope
		out.Reason = SemanticExtractionReasonScopeDisabled
	}
	out.Detail = defaultSemanticExtractionDetail(out.State)
	if out.State != SemanticExtractionAvailable {
		out.DocumentationObservationsEnabled = false
		out.CodeHintsEnabled = false
	}
	return out
}

func semanticProfilesConfigured(profiles []SemanticProviderProfileStatus) bool {
	for _, profile := range profiles {
		if semanticProfileConfigured(profile) {
			return true
		}
	}
	return false
}

func semanticProfilesUnhealthy(profiles []SemanticProviderProfileStatus) bool {
	for _, profile := range profiles {
		if semanticProfileUnhealthy(profile) {
			return true
		}
	}
	return false
}

func semanticProfilesHaveAnySourcePolicy(profiles []SemanticProviderProfileStatus) bool {
	for _, profile := range profiles {
		if semanticProfileConfigured(profile) && profile.SourcePolicyConfigured {
			return true
		}
	}
	return false
}

func semanticProfilesAllowSource(profiles []SemanticProviderProfileStatus, sourceClass string) bool {
	for _, profile := range profiles {
		if semanticProfileAllowsSource(profile, sourceClass) {
			return true
		}
	}
	return false
}

func isSemanticExtractionState(state string) bool {
	return slices.Contains(semanticExtractionStates, state)
}

func defaultSemanticExtractionReason(state string) string {
	switch state {
	case SemanticExtractionAvailable:
		return SemanticExtractionReasonProviderConfigured
	case SemanticExtractionAvailableButDisabledForScope:
		return SemanticExtractionReasonScopeDisabled
	case SemanticExtractionDisabledByPolicy:
		return SemanticExtractionReasonPolicyDisabled
	case SemanticExtractionProviderUnhealthy:
		return SemanticExtractionReasonProviderUnhealthy
	default:
		return SemanticExtractionReasonProviderNotConfigured
	}
}

func defaultSemanticExtractionDetail(state string) string {
	switch state {
	case SemanticExtractionAvailable:
		return "semantic extraction provider is configured; deterministic evidence remains the admission gate for code hints"
	case SemanticExtractionAvailableButDisabledForScope:
		return "semantic extraction provider is configured, but this scope is disabled; deterministic indexing and reads are unaffected"
	case SemanticExtractionDisabledByPolicy:
		return "semantic extraction is disabled by operator policy; deterministic indexing and reads are unaffected"
	case SemanticExtractionProviderUnhealthy:
		return "semantic extraction provider is unhealthy; deterministic indexing and reads are unaffected"
	default:
		return DefaultSemanticExtractionStatus().Detail
	}
}

func renderSemanticExtractionLine(snapshot SemanticExtractionStatus) string {
	status := normalizeSemanticExtractionStatus(snapshot)
	line := fmt.Sprintf(
		"Semantic extraction: state=%s reason=%s code_hints=%s documentation_observations=%s deterministic_paths=%s provider_profiles=%d",
		status.State,
		status.Reason,
		enabledText(status.CodeHintsEnabled),
		enabledText(status.DocumentationObservationsEnabled),
		affectedText(status.DeterministicPathsAffected),
		len(status.ProviderProfiles),
	)
	if len(status.ProviderProfiles) == 0 {
		return line
	}

	profileParts := make([]string, 0, len(status.ProviderProfiles))
	for _, profile := range status.ProviderProfiles {
		profileParts = append(profileParts, fmt.Sprintf(
			"profile=%s provider=%s credential_source=%s credential_configured=%t state=%s source_policy=%t sources=%s",
			profile.ProfileID,
			profile.ProviderKind,
			profile.CredentialSourceKind,
			profile.CredentialConfigured,
			profile.State,
			profile.SourcePolicyConfigured,
			strings.Join(profile.SourceClasses, ","),
		))
	}
	return line + " " + strings.Join(profileParts, "; ")
}

func enabledText(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func affectedText(affected bool) string {
	if affected {
		return "affected"
	}
	return "unaffected"
}

type semanticExtractionJSON struct {
	State                            string                        `json:"state"`
	Reason                           string                        `json:"reason"`
	Detail                           string                        `json:"detail,omitempty"`
	ProviderConfigured               bool                          `json:"provider_configured"`
	DocumentationObservationsEnabled bool                          `json:"documentation_observations_enabled"`
	CodeHintsEnabled                 bool                          `json:"code_hints_enabled"`
	DeterministicPathsAffected       bool                          `json:"deterministic_paths_affected"`
	UpdatedAt                        string                        `json:"updated_at,omitempty"`
	ProviderProfiles                 []semanticProviderProfileJSON `json:"provider_profiles,omitempty"`
	SupportedStates                  []string                      `json:"supported_states"`
	SupportedProviderProfileStates   []string                      `json:"supported_provider_profile_states"`
}

func semanticExtractionStatusJSON(snapshot SemanticExtractionStatus) semanticExtractionJSON {
	status := normalizeSemanticExtractionStatus(snapshot)
	out := semanticExtractionJSON{
		State:                            status.State,
		Reason:                           status.Reason,
		Detail:                           status.Detail,
		ProviderConfigured:               status.ProviderConfigured,
		DocumentationObservationsEnabled: status.DocumentationObservationsEnabled,
		CodeHintsEnabled:                 status.CodeHintsEnabled,
		DeterministicPathsAffected:       status.DeterministicPathsAffected,
		ProviderProfiles:                 semanticProviderProfilesJSON(status.ProviderProfiles),
		SupportedStates:                  SemanticExtractionSupportedStates(),
		SupportedProviderProfileStates:   SemanticProviderProfileSupportedStates(),
	}
	if !status.UpdatedAt.IsZero() {
		out.UpdatedAt = status.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return out
}
