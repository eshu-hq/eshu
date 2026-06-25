// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticprofile

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/status"
)

const (
	// EnvProviderProfilesJSON names the optional JSON profile registry.
	EnvProviderProfilesJSON = "ESHU_SEMANTIC_PROVIDER_PROFILES_JSON"
)

const (
	// ProviderAnthropic identifies an Anthropic-hosted semantic provider.
	ProviderAnthropic = "anthropic"
	// ProviderOpenAICompatible identifies an OpenAI-compatible provider.
	ProviderOpenAICompatible = "openai_compatible"
	// ProviderDeepSeek identifies a DeepSeek provider profile.
	ProviderDeepSeek = "deepseek"
	// ProviderMiniMax identifies a MiniMax provider profile.
	ProviderMiniMax = "minimax"
	// ProviderGemini identifies a Gemini provider profile.
	ProviderGemini = "gemini"
	// ProviderBedrock identifies an AWS Bedrock provider profile.
	ProviderBedrock = "bedrock"
	// ProviderAzureOpenAI identifies an Azure OpenAI provider profile.
	ProviderAzureOpenAI = "azure_openai"
	// ProviderOllama identifies an Ollama or local-gateway provider profile.
	ProviderOllama = "ollama"
	// ProviderInternalGateway identifies an internal semantic gateway profile.
	ProviderInternalGateway = "internal_gateway"
)

const (
	// CredentialSourceKubernetesSecret means credentials live in a Kubernetes Secret.
	CredentialSourceKubernetesSecret = "kubernetes_secret" // #nosec G101 -- credential source kind identifier, not a credential value
	// CredentialSourceVaultSecretHandle means credentials live behind a Vault-like handle.
	CredentialSourceVaultSecretHandle = "vault_secret_handle"
	// CredentialSourceEnvironmentVariable means credentials are referenced by env var name.
	CredentialSourceEnvironmentVariable = "environment_variable"
	// CredentialSourceCloudWorkloadIdentity means cloud workload identity supplies auth.
	CredentialSourceCloudWorkloadIdentity = "cloud_workload_identity"
	// CredentialSourceLocalDevProfile means local development profile configuration supplies auth.
	CredentialSourceLocalDevProfile = "local_dev_profile" // #nosec G101 -- credential source kind identifier, not a credential value
)

const (
	// SourceDocumentation allows documentation semantic observations.
	SourceDocumentation = "documentation"
	// SourceDiagramsImages allows image and diagram semantic observations.
	SourceDiagramsImages = "diagrams_images"
	// SourceTicketsChat allows ticket and chat semantic observations.
	SourceTicketsChat = "tickets_chat"
	// SourceCodeHints allows assistant-mediated code relationship hints.
	SourceCodeHints = "code_hints"
	// SourceSearchDocuments allows curated search-document embedding builds.
	SourceSearchDocuments = "search_documents"
	// SourceAgentReasoning allows agent reasoning and planning semantic observations.
	SourceAgentReasoning = "agent_reasoning"
)

var (
	supportedProviderKinds = []string{
		ProviderAnthropic,
		ProviderOpenAICompatible,
		ProviderDeepSeek,
		ProviderMiniMax,
		ProviderGemini,
		ProviderBedrock,
		ProviderAzureOpenAI,
		ProviderOllama,
		ProviderInternalGateway,
	}
	supportedCredentialSources = []string{
		CredentialSourceKubernetesSecret,
		CredentialSourceVaultSecretHandle,
		CredentialSourceEnvironmentVariable,
		CredentialSourceCloudWorkloadIdentity,
		CredentialSourceLocalDevProfile,
	}
	supportedSourceClasses = []string{
		SourceDocumentation,
		SourceDiagramsImages,
		SourceTicketsChat,
		SourceCodeHints,
		SourceSearchDocuments,
		SourceAgentReasoning,
	}
	envVarNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

// CredentialSource identifies where a provider credential can be loaded later.
type CredentialSource struct {
	Kind   string `json:"kind"`
	Handle string `json:"handle,omitempty"`
}

// ProviderHealth carries optional externally supplied health metadata.
type ProviderHealth struct {
	State  string `json:"state,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// ProviderProfile captures one semantic provider profile config row.
type ProviderProfile struct {
	ProfileID              string           `json:"profile_id"`
	DisplayName            string           `json:"display_name,omitempty"`
	ProviderKind           string           `json:"provider_kind"`
	CredentialSource       CredentialSource `json:"credential_source"`
	ModelID                string           `json:"model_id"`
	EmbeddingDimensions    int              `json:"embedding_dimensions,omitempty"`
	EndpointProfileID      string           `json:"endpoint_profile_id,omitempty"`
	SourceClasses          []string         `json:"source_classes"`
	SourcePolicyConfigured bool             `json:"source_policy_configured"`
	Health                 ProviderHealth   `json:"health,omitempty"`
}

type profileConfigEnvelope struct {
	Profiles []ProviderProfile `json:"profiles"`
}

// LoadStatusesFromEnv reads provider profile config from getenv and returns a
// redacted status projection. It does not read provider credential values.
func LoadStatusesFromEnv(
	getenv func(string) string,
) ([]status.SemanticProviderProfileStatus, error) {
	if getenv == nil {
		return nil, nil
	}
	raw := strings.TrimSpace(getenv(EnvProviderProfilesJSON))
	if raw == "" {
		return nil, nil
	}
	profiles, err := ParseProfilesJSON(raw)
	if err != nil {
		return nil, err
	}
	return ProviderStatuses(profiles), nil
}

// ParseProfilesJSON parses and validates semantic provider profiles.
func ParseProfilesJSON(raw string) ([]ProviderProfile, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var profiles []ProviderProfile
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &profiles); err != nil {
			return nil, fmt.Errorf("parse %s: %w", EnvProviderProfilesJSON, err)
		}
	} else {
		var envelope profileConfigEnvelope
		if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
			return nil, fmt.Errorf("parse %s: %w", EnvProviderProfilesJSON, err)
		}
		profiles = envelope.Profiles
	}

	normalized := make([]ProviderProfile, 0, len(profiles))
	seen := make(map[string]struct{}, len(profiles))
	for i, profile := range profiles {
		row, err := normalizeProfile(profile)
		if err != nil {
			return nil, fmt.Errorf("profile[%d]: %w", i, err)
		}
		if _, ok := seen[row.ProfileID]; ok {
			return nil, fmt.Errorf("profile[%d].profile_id %q is duplicated", i, row.ProfileID)
		}
		seen[row.ProfileID] = struct{}{}
		normalized = append(normalized, row)
	}
	return normalized, nil
}

// ProviderStatuses returns redacted operator status rows for the profile config.
func ProviderStatuses(profiles []ProviderProfile) []status.SemanticProviderProfileStatus {
	if len(profiles) == 0 {
		return nil
	}
	rows := make([]status.SemanticProviderProfileStatus, 0, len(profiles))
	for _, profile := range profiles {
		state := status.SemanticProviderProfileConfigured
		reason := "provider_profile_configured"
		detail := ""
		switch strings.TrimSpace(profile.Health.State) {
		case status.SemanticProviderProfileHealthy:
			state = status.SemanticProviderProfileHealthy
			reason = "provider_profile_healthy"
		case status.SemanticProviderProfileUnhealthy:
			state = status.SemanticProviderProfileUnhealthy
			reason = "provider_profile_unhealthy"
			detail = strings.TrimSpace(profile.Health.Detail)
		}
		rows = append(rows, status.SemanticProviderProfileStatus{
			ProfileID:              profile.ProfileID,
			DisplayName:            profile.DisplayName,
			ProviderKind:           profile.ProviderKind,
			CredentialSourceKind:   profile.CredentialSource.Kind,
			CredentialConfigured:   credentialConfigured(profile.CredentialSource),
			ModelID:                profile.ModelID,
			EmbeddingDimensions:    profile.EmbeddingDimensions,
			EndpointProfileID:      profile.EndpointProfileID,
			SourceClasses:          slices.Clone(profile.SourceClasses),
			SourcePolicyConfigured: profile.SourcePolicyConfigured,
			State:                  state,
			Reason:                 reason,
			Detail:                 detail,
		})
	}
	return rows
}

func normalizeProfile(profile ProviderProfile) (ProviderProfile, error) {
	out := ProviderProfile{
		ProfileID:              strings.TrimSpace(profile.ProfileID),
		DisplayName:            strings.TrimSpace(profile.DisplayName),
		ProviderKind:           strings.TrimSpace(profile.ProviderKind),
		CredentialSource:       normalizeCredentialSource(profile.CredentialSource),
		ModelID:                strings.TrimSpace(profile.ModelID),
		EmbeddingDimensions:    profile.EmbeddingDimensions,
		EndpointProfileID:      strings.TrimSpace(profile.EndpointProfileID),
		SourcePolicyConfigured: profile.SourcePolicyConfigured,
		Health: ProviderHealth{
			State:  strings.TrimSpace(profile.Health.State),
			Detail: strings.TrimSpace(profile.Health.Detail),
		},
	}
	if out.ProfileID == "" {
		return ProviderProfile{}, fmt.Errorf("profile_id is required")
	}
	if !isSupported(out.ProviderKind, supportedProviderKinds) {
		return ProviderProfile{}, fmt.Errorf("provider_kind %q is unsupported", out.ProviderKind)
	}
	if out.ModelID == "" {
		return ProviderProfile{}, fmt.Errorf("model_id is required")
	}
	if !isSupported(out.CredentialSource.Kind, supportedCredentialSources) {
		return ProviderProfile{}, fmt.Errorf("credential_source.kind %q is unsupported", out.CredentialSource.Kind)
	}
	if err := validateCredentialSource(out.CredentialSource); err != nil {
		return ProviderProfile{}, err
	}
	sourceClasses, err := normalizeSourceClasses(profile.SourceClasses)
	if err != nil {
		return ProviderProfile{}, err
	}
	if len(sourceClasses) == 0 {
		return ProviderProfile{}, fmt.Errorf("source_classes must include at least one source class")
	}
	out.SourceClasses = sourceClasses
	if out.Health.State != "" && !slices.Contains(status.SemanticProviderProfileSupportedStates(), out.Health.State) {
		return ProviderProfile{}, fmt.Errorf("health.state %q is unsupported", out.Health.State)
	}
	return out, nil
}

func normalizeCredentialSource(source CredentialSource) CredentialSource {
	return CredentialSource{
		Kind:   strings.TrimSpace(source.Kind),
		Handle: strings.TrimSpace(source.Handle),
	}
}

func validateCredentialSource(source CredentialSource) error {
	if looksLikeProviderCredential(source.Handle) {
		return fmt.Errorf("credential_source.handle must be a secret handle, not a provider key")
	}
	switch source.Kind {
	case CredentialSourceCloudWorkloadIdentity:
		return nil
	case CredentialSourceEnvironmentVariable:
		if !envVarNamePattern.MatchString(source.Handle) {
			return fmt.Errorf("credential_source.handle must be an environment variable name")
		}
	default:
		if source.Handle == "" {
			return fmt.Errorf("credential_source.handle is required")
		}
	}
	return nil
}

func looksLikeProviderCredential(handle string) bool {
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return false
	}
	lower := strings.ToLower(handle)
	return strings.HasPrefix(lower, "sk-") ||
		strings.HasPrefix(lower, "sk_live_") ||
		strings.HasPrefix(handle, "AIza") ||
		strings.HasPrefix(handle, "AKIA")
}

func normalizeSourceClasses(sourceClasses []string) ([]string, error) {
	seen := make(map[string]struct{}, len(sourceClasses))
	normalized := make([]string, 0, len(sourceClasses))
	for _, sourceClass := range sourceClasses {
		sourceClass = strings.TrimSpace(sourceClass)
		if sourceClass == "" {
			continue
		}
		if !isSupported(sourceClass, supportedSourceClasses) {
			return nil, fmt.Errorf("source_classes contains unsupported class %q", sourceClass)
		}
		if _, ok := seen[sourceClass]; ok {
			continue
		}
		seen[sourceClass] = struct{}{}
		normalized = append(normalized, sourceClass)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func credentialConfigured(source CredentialSource) bool {
	if source.Kind == CredentialSourceCloudWorkloadIdentity {
		return true
	}
	return source.Handle != ""
}

func isSupported(value string, allowed []string) bool {
	return slices.Contains(allowed, value)
}
