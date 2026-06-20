package provider

import (
	"fmt"
	"slices"

	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
)

// defaultMiniMaxBaseURL is the public MiniMax international OpenAI-compatible
// chat completion endpoint used when the profile's EndpointProfileID is empty.
const defaultMiniMaxBaseURL = "https://api.minimax.io"

// defaultDeepSeekBaseURL is the public DeepSeek chat completion endpoint used when
// the profile's EndpointProfileID is empty.
const defaultDeepSeekBaseURL = "https://api.deepseek.com"

// NewAdapter constructs an Adapter from a semantic provider profile.
//
// Profile selection rules:
//   - The profile must include semanticprofile.SourceAgentReasoning in its
//     SourceClasses. Profiles that do not declare agent_reasoning are rejected
//     at construction time so callers cannot accidentally route non-agent
//     profiles through the ask pipeline.
//   - Credentials are resolved from profile.CredentialSource via the supplied
//     getenv function. Cloud workload identity profiles resolve to an empty
//     credential string (authentication is handled out-of-band).
//   - profile.EndpointProfileID is interpreted as a base URL override. When it
//     is non-empty it is passed directly to the underlying adapter. When it is
//     empty the factory either applies a provider-specific default (MiniMax,
//     DeepSeek, Anthropic) or returns an error requiring the caller to supply
//     an explicit endpoint (all other OpenAI-compatible kinds).
//
// Adapter routing:
//   - anthropic               → Anthropic Messages API adapter (newAnthropicAdapter).
//     Defaults to https://api.anthropic.com when EndpointProfileID is empty.
//   - bedrock                 → Anthropic Messages API adapter (newAnthropicAdapter).
//     Bedrock exposes the same wire shape but has no single public endpoint.
//     EndpointProfileID is required; an empty value returns an error.
//   - openai_compatible,
//     gemini, azure_openai,
//     ollama, internal_gateway → OpenAI-compatible adapter (newOpenAICompatAdapter).
//     An explicit EndpointProfileID is required; the factory returns an error
//     when none is provided.
//   - minimax, deepseek        → OpenAI-compatible adapter with a documented
//     default base URL when EndpointProfileID is empty:
//     MiniMax → https://api.minimax.chat
//     DeepSeek → https://api.deepseek.com
//
// The returned Adapter is safe to use concurrently; it holds no mutable state
// and creates no background goroutines.
func NewAdapter(profile semanticprofile.ProviderProfile, getenv func(string) string) (Adapter, error) {
	if !slices.Contains(profile.SourceClasses, semanticprofile.SourceAgentReasoning) {
		id := profile.ProfileID
		if id == "" {
			id = profile.ModelID
		}
		return nil, fmt.Errorf("ask/provider: profile %q is not an agent_reasoning provider", id)
	}

	cred, err := resolveCredential(profile.CredentialSource, getenv)
	if err != nil {
		return nil, fmt.Errorf("ask/provider: %w", err)
	}

	endpoint := profile.EndpointProfileID

	switch profile.ProviderKind {
	case semanticprofile.ProviderAnthropic:
		// newAnthropicAdapter defaults to the Anthropic production endpoint
		// when baseURL is empty, so we pass endpoint as-is.
		return newAnthropicAdapter(endpoint, cred, profile.ModelID, nil), nil

	case semanticprofile.ProviderBedrock:
		// Bedrock does not have a single public endpoint; the caller must
		// supply an explicit regional base URL via endpoint_profile_id.
		if endpoint == "" {
			return nil, fmt.Errorf("ask/provider: bedrock provider requires an explicit endpoint_profile_id base URL")
		}
		return newAnthropicAdapter(endpoint, cred, profile.ModelID, nil), nil

	case semanticprofile.ProviderMiniMax:
		if endpoint == "" {
			endpoint = defaultMiniMaxBaseURL
		}
		return newOpenAICompatAdapter(endpoint, cred, profile.ModelID, nil), nil

	case semanticprofile.ProviderDeepSeek:
		if endpoint == "" {
			endpoint = defaultDeepSeekBaseURL
		}
		return newOpenAICompatAdapter(endpoint, cred, profile.ModelID, nil), nil

	case semanticprofile.ProviderOpenAICompatible,
		semanticprofile.ProviderGemini,
		semanticprofile.ProviderAzureOpenAI,
		semanticprofile.ProviderOllama,
		semanticprofile.ProviderInternalGateway:
		if endpoint == "" {
			return nil, fmt.Errorf("ask/provider: provider_kind %q requires an explicit base URL; set endpoint_profile_id in the provider profile", profile.ProviderKind)
		}
		return newOpenAICompatAdapter(endpoint, cred, profile.ModelID, nil), nil

	default:
		return nil, fmt.Errorf("ask/provider: provider kind %q has no ask adapter", profile.ProviderKind)
	}
}
