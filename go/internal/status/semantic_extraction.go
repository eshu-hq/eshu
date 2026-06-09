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
	state := strings.TrimSpace(snapshot.State)
	if state == "" {
		return DefaultSemanticExtractionStatus()
	}
	if !isSemanticExtractionState(state) {
		out := DefaultSemanticExtractionStatus()
		out.Reason = SemanticExtractionReasonInvalidState
		out.Detail = fmt.Sprintf("semantic extraction status %q is unsupported; treating semantic extraction as unavailable", state)
		out.UpdatedAt = snapshot.UpdatedAt
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
	return fmt.Sprintf(
		"Semantic extraction: state=%s reason=%s code_hints=%s documentation_observations=%s deterministic_paths=%s",
		status.State,
		status.Reason,
		enabledText(status.CodeHintsEnabled),
		enabledText(status.DocumentationObservationsEnabled),
		affectedText(status.DeterministicPathsAffected),
	)
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
	State                            string   `json:"state"`
	Reason                           string   `json:"reason"`
	Detail                           string   `json:"detail,omitempty"`
	ProviderConfigured               bool     `json:"provider_configured"`
	DocumentationObservationsEnabled bool     `json:"documentation_observations_enabled"`
	CodeHintsEnabled                 bool     `json:"code_hints_enabled"`
	DeterministicPathsAffected       bool     `json:"deterministic_paths_affected"`
	UpdatedAt                        string   `json:"updated_at,omitempty"`
	SupportedStates                  []string `json:"supported_states"`
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
		SupportedStates:                  SemanticExtractionSupportedStates(),
	}
	if !status.UpdatedAt.IsZero() {
		out.UpdatedAt = status.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return out
}
