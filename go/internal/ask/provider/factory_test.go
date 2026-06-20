package provider

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
)

// staticEnv returns a getenv function backed by the given map.
func staticEnv(env map[string]string) func(string) string {
	return func(key string) string {
		return env[key]
	}
}

// agentReasoningProfile builds a minimal ProviderProfile with agent_reasoning as a source class.
func agentReasoningProfile(kind, modelID, endpointProfileID string, cred semanticprofile.CredentialSource) semanticprofile.ProviderProfile {
	return semanticprofile.ProviderProfile{
		ProfileID:         "test-" + kind,
		ProviderKind:      kind,
		ModelID:           modelID,
		EndpointProfileID: endpointProfileID,
		CredentialSource:  cred,
		SourceClasses:     []string{semanticprofile.SourceAgentReasoning},
	}
}

// envCred returns a CredentialSource of type environment_variable referencing the given env var name.
func envCred(envVar string) semanticprofile.CredentialSource {
	return semanticprofile.CredentialSource{
		Kind:   semanticprofile.CredentialSourceEnvironmentVariable,
		Handle: envVar,
	}
}

// workloadCred returns a CredentialSource of type cloud_workload_identity (no secret needed).
func workloadCred() semanticprofile.CredentialSource {
	return semanticprofile.CredentialSource{
		Kind: semanticprofile.CredentialSourceCloudWorkloadIdentity,
	}
}

func TestNewAdapter_MissingAgentReasoningSourceClass(t *testing.T) {
	t.Parallel()
	profile := semanticprofile.ProviderProfile{
		ProfileID:    "test-anthropic",
		ProviderKind: semanticprofile.ProviderAnthropic,
		ModelID:      "claude-3-5-sonnet-20241022",
		CredentialSource: semanticprofile.CredentialSource{
			Kind:   semanticprofile.CredentialSourceEnvironmentVariable,
			Handle: "ANTHROPIC_API_KEY",
		},
		SourceClasses: []string{"code_search"}, // no agent_reasoning
	}

	_, err := NewAdapter(profile, staticEnv(map[string]string{"ANTHROPIC_API_KEY": "sk-test"}))
	if err == nil {
		t.Fatal("NewAdapter: expected error for missing agent_reasoning source class, got nil")
	}
	if !strings.Contains(err.Error(), "agent_reasoning") {
		t.Errorf("NewAdapter: error %q does not mention agent_reasoning", err.Error())
	}
}

func TestNewAdapter_AnthropicProfile(t *testing.T) {
	t.Parallel()
	const model = "claude-3-5-sonnet-20241022"
	profile := agentReasoningProfile(
		semanticprofile.ProviderAnthropic,
		model,
		"", // no endpoint override; uses the default Anthropic URL
		envCred("ANTHROPIC_KEY"),
	)

	adapter, err := NewAdapter(profile, staticEnv(map[string]string{"ANTHROPIC_KEY": "sk-ant-test"}))
	if err != nil {
		t.Fatalf("NewAdapter: unexpected error for anthropic profile: %v", err)
	}
	if adapter == nil {
		t.Fatal("NewAdapter: returned nil adapter for anthropic profile")
	}
	if got := adapter.ModelID(); got != model {
		t.Errorf("NewAdapter: ModelID() = %q, want %q", got, model)
	}
}

func TestNewAdapter_BedrockProfile(t *testing.T) {
	t.Parallel()
	const model = "anthropic.claude-3-5-sonnet-20241022-v2:0"
	profile := agentReasoningProfile(
		semanticprofile.ProviderBedrock,
		model,
		"https://bedrock.us-east-1.amazonaws.com",
		workloadCred(),
	)

	adapter, err := NewAdapter(profile, staticEnv(map[string]string{}))
	if err != nil {
		t.Fatalf("NewAdapter: unexpected error for bedrock profile: %v", err)
	}
	if adapter == nil {
		t.Fatal("NewAdapter: returned nil adapter for bedrock profile")
	}
	if got := adapter.ModelID(); got != model {
		t.Errorf("NewAdapter: ModelID() = %q, want %q", got, model)
	}
}

func TestNewAdapter_BedrockProfile_NoEndpoint_Error(t *testing.T) {
	t.Parallel()
	const model = "anthropic.claude-3-5-sonnet-20241022-v2:0"
	profile := agentReasoningProfile(
		semanticprofile.ProviderBedrock,
		model,
		"", // no EndpointProfileID — must error for bedrock
		workloadCred(),
	)

	_, err := NewAdapter(profile, staticEnv(map[string]string{}))
	if err == nil {
		t.Fatal("NewAdapter: expected error for bedrock profile without endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "endpoint_profile_id") {
		t.Errorf("NewAdapter: error %q does not mention endpoint_profile_id", err.Error())
	}
}

func TestNewAdapter_MiniMaxProfile_DefaultEndpoint(t *testing.T) {
	t.Parallel()
	const model = "MiniMax-Text-01"
	profile := agentReasoningProfile(
		semanticprofile.ProviderMiniMax,
		model,
		"", // no EndpointProfileID — factory should apply the documented default
		envCred("MINIMAX_API_KEY"),
	)

	adapter, err := NewAdapter(profile, staticEnv(map[string]string{"MINIMAX_API_KEY": "mm-key-test"}))
	if err != nil {
		t.Fatalf("NewAdapter: unexpected error for minimax profile: %v", err)
	}
	if adapter == nil {
		t.Fatal("NewAdapter: returned nil adapter for minimax profile")
	}
	if got := adapter.ModelID(); got != model {
		t.Errorf("NewAdapter: ModelID() = %q, want %q", got, model)
	}
}

func TestNewAdapter_DeepSeekProfile_DefaultEndpoint(t *testing.T) {
	t.Parallel()
	const model = "deepseek-chat"
	profile := agentReasoningProfile(
		semanticprofile.ProviderDeepSeek,
		model,
		"", // no EndpointProfileID — factory should apply the documented default
		envCred("DEEPSEEK_API_KEY"),
	)

	adapter, err := NewAdapter(profile, staticEnv(map[string]string{"DEEPSEEK_API_KEY": "ds-key-test"}))
	if err != nil {
		t.Fatalf("NewAdapter: unexpected error for deepseek profile: %v", err)
	}
	if adapter == nil {
		t.Fatal("NewAdapter: returned nil adapter for deepseek profile")
	}
	if got := adapter.ModelID(); got != model {
		t.Errorf("NewAdapter: ModelID() = %q, want %q", got, model)
	}
}

func TestNewAdapter_OpenAICompatible_NoEndpoint_Error(t *testing.T) {
	t.Parallel()
	profile := agentReasoningProfile(
		semanticprofile.ProviderOpenAICompatible,
		"gpt-4o",
		"", // no EndpointProfileID — should error for generic openai_compatible
		envCred("OPENAI_KEY"),
	)

	_, err := NewAdapter(profile, staticEnv(map[string]string{"OPENAI_KEY": "sk-openai-test"}))
	if err == nil {
		t.Fatal("NewAdapter: expected error for openai_compatible profile without endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "endpoint_profile_id") {
		t.Errorf("NewAdapter: error %q does not mention endpoint_profile_id", err.Error())
	}
}

func TestNewAdapter_Gemini_NoEndpoint_Error(t *testing.T) {
	t.Parallel()
	profile := agentReasoningProfile(
		semanticprofile.ProviderGemini,
		"gemini-1.5-pro",
		"", // no EndpointProfileID — should error (not a provider with a known default)
		envCred("GEMINI_KEY"),
	)

	_, err := NewAdapter(profile, staticEnv(map[string]string{"GEMINI_KEY": "gm-test"}))
	if err == nil {
		t.Fatal("NewAdapter: expected error for gemini profile without endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "endpoint_profile_id") {
		t.Errorf("NewAdapter: error %q does not mention endpoint_profile_id", err.Error())
	}
}

func TestNewAdapter_OpenAICompatible_WithEndpoint(t *testing.T) {
	t.Parallel()
	const model = "gpt-4o"
	profile := agentReasoningProfile(
		semanticprofile.ProviderOpenAICompatible,
		model,
		"https://api.openai.com",
		envCred("OPENAI_KEY"),
	)

	adapter, err := NewAdapter(profile, staticEnv(map[string]string{"OPENAI_KEY": "sk-openai-test"}))
	if err != nil {
		t.Fatalf("NewAdapter: unexpected error for openai_compatible with endpoint: %v", err)
	}
	if adapter == nil {
		t.Fatal("NewAdapter: returned nil adapter for openai_compatible with endpoint")
	}
	if got := adapter.ModelID(); got != model {
		t.Errorf("NewAdapter: ModelID() = %q, want %q", got, model)
	}
}

func TestNewAdapter_UnknownProviderKind_Error(t *testing.T) {
	t.Parallel()
	profile := agentReasoningProfile(
		"hypothetical_future_provider",
		"some-model",
		"",
		workloadCred(),
	)

	_, err := NewAdapter(profile, staticEnv(map[string]string{}))
	if err == nil {
		t.Fatal("NewAdapter: expected error for unknown provider kind, got nil")
	}
	if !strings.Contains(err.Error(), "hypothetical_future_provider") {
		t.Errorf("NewAdapter: error %q does not name the unknown kind", err.Error())
	}
}
