package semanticprofile_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestLoadStatusesFromEnvReturnsNilForBlankConfig(t *testing.T) {
	t.Parallel()

	statuses, err := semanticprofile.LoadStatusesFromEnv(func(string) string {
		return ""
	})
	if err != nil {
		t.Fatalf("LoadStatusesFromEnv() error = %v, want nil", err)
	}
	if len(statuses) != 0 {
		t.Fatalf("LoadStatusesFromEnv() len = %d, want 0", len(statuses))
	}
}

func TestLoadStatusesFromEnvProjectsRedactedDeepSeekProfile(t *testing.T) {
	t.Parallel()

	raw := `[
		{
			"profile_id": "semantic-docs-default",
			"display_name": "Documentation semantic default",
			"provider_kind": "deepseek",
			"credential_source": {
				"kind": "environment_variable",
				"handle": "DEEPSEEK_API_KEY"
			},
			"model_id": "deepseek-chat",
			"endpoint_profile_id": "deepseek-public-api",
			"source_classes": ["documentation"],
			"source_policy_configured": true
		}
	]`

	statuses, err := semanticprofile.LoadStatusesFromEnv(func(key string) string {
		if key == semanticprofile.EnvProviderProfilesJSON {
			return raw
		}
		return ""
	})
	if err != nil {
		t.Fatalf("LoadStatusesFromEnv() error = %v, want nil", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("LoadStatusesFromEnv() len = %d, want 1", len(statuses))
	}

	got := statuses[0]
	if got.ProfileID != "semantic-docs-default" {
		t.Fatalf("ProfileID = %q, want semantic-docs-default", got.ProfileID)
	}
	if got.ProviderKind != semanticprofile.ProviderDeepSeek {
		t.Fatalf("ProviderKind = %q, want %q", got.ProviderKind, semanticprofile.ProviderDeepSeek)
	}
	if got.CredentialSourceKind != semanticprofile.CredentialSourceEnvironmentVariable {
		t.Fatalf("CredentialSourceKind = %q, want %q", got.CredentialSourceKind, semanticprofile.CredentialSourceEnvironmentVariable)
	}
	if !got.CredentialConfigured {
		t.Fatal("CredentialConfigured = false, want true")
	}
	if !got.SourcePolicyConfigured {
		t.Fatal("SourcePolicyConfigured = false, want true")
	}
	if got.State != status.SemanticProviderProfileConfigured {
		t.Fatalf("State = %q, want %q", got.State, status.SemanticProviderProfileConfigured)
	}

	encoded, err := json.Marshal(statuses)
	if err != nil {
		t.Fatalf("json.Marshal(statuses) error = %v, want nil", err)
	}
	if strings.Contains(string(encoded), "DEEPSEEK_API_KEY") {
		t.Fatalf("marshaled statuses leaked credential handle: %s", encoded)
	}
}

func TestLoadStatusesFromEnvAcceptsSearchDocumentSourceClass(t *testing.T) {
	t.Parallel()

	raw := `[
		{
			"profile_id": "semantic-search-default",
			"provider_kind": "internal_gateway",
			"credential_source": {
				"kind": "cloud_workload_identity"
			},
			"model_id": "search-embed-v1",
			"endpoint_profile_id": "semantic-search-gateway",
			"source_classes": ["search_documents"],
			"source_policy_configured": true
		}
	]`

	statuses, err := semanticprofile.LoadStatusesFromEnv(func(key string) string {
		if key == semanticprofile.EnvProviderProfilesJSON {
			return raw
		}
		return ""
	})
	if err != nil {
		t.Fatalf("LoadStatusesFromEnv() error = %v, want nil", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("LoadStatusesFromEnv() len = %d, want 1", len(statuses))
	}
	if got, want := statuses[0].SourceClasses, []string{semanticprofile.SourceSearchDocuments}; !slices.Equal(got, want) {
		t.Fatalf("SourceClasses = %#v, want %#v", got, want)
	}
}

func TestLoadStatusesFromEnvRejectsEnvironmentVariableCredentialValue(t *testing.T) {
	t.Parallel()

	raw := `[
		{
			"profile_id": "semantic-docs-default",
			"provider_kind": "deepseek",
			"credential_source": {
				"kind": "environment_variable",
				"handle": "sk-live-123"
			},
			"model_id": "deepseek-chat",
			"source_classes": ["documentation"],
			"source_policy_configured": true
		}
	]`

	_, err := semanticprofile.LoadStatusesFromEnv(func(key string) string {
		if key == semanticprofile.EnvProviderProfilesJSON {
			return raw
		}
		return ""
	})
	if err == nil {
		t.Fatal("LoadStatusesFromEnv() error = nil, want invalid env var handle error")
	}
	if !strings.Contains(err.Error(), "credential_source.handle") {
		t.Fatalf("LoadStatusesFromEnv() error = %q, want credential_source.handle context", err)
	}
}

func TestLoadStatusesFromEnvRejectsRawProviderKeyAsSecretHandle(t *testing.T) {
	t.Parallel()

	raw := `[
		{
			"profile_id": "semantic-docs-default",
			"provider_kind": "deepseek",
			"credential_source": {
				"kind": "kubernetes_secret",
				"handle": "sk-live-123"
			},
			"model_id": "deepseek-chat",
			"source_classes": ["documentation"],
			"source_policy_configured": true
		}
	]`

	_, err := semanticprofile.LoadStatusesFromEnv(func(key string) string {
		if key == semanticprofile.EnvProviderProfilesJSON {
			return raw
		}
		return ""
	})
	if err == nil {
		t.Fatal("LoadStatusesFromEnv() error = nil, want raw credential rejection")
	}
	if !strings.Contains(err.Error(), "not a provider key") {
		t.Fatalf("LoadStatusesFromEnv() error = %q, want raw credential context", err)
	}
}

func TestLoadStatusesFromEnvRejectsUnknownProfileDimensions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "provider",
			raw: `[
				{
					"profile_id": "semantic-docs-default",
					"provider_kind": "not-a-provider",
					"credential_source": {"kind": "environment_variable", "handle": "DEEPSEEK_API_KEY"},
					"model_id": "model",
					"source_classes": ["documentation"],
					"source_policy_configured": true
				}
			]`,
			want: "provider_kind",
		},
		{
			name: "credential source",
			raw: `[
				{
					"profile_id": "semantic-docs-default",
					"provider_kind": "deepseek",
					"credential_source": {"kind": "not-a-source", "handle": "DEEPSEEK_API_KEY"},
					"model_id": "model",
					"source_classes": ["documentation"],
					"source_policy_configured": true
				}
			]`,
			want: "credential_source.kind",
		},
		{
			name: "source class",
			raw: `[
				{
					"profile_id": "semantic-docs-default",
					"provider_kind": "deepseek",
					"credential_source": {"kind": "environment_variable", "handle": "DEEPSEEK_API_KEY"},
					"model_id": "model",
					"source_classes": ["unknown-source"],
					"source_policy_configured": true
				}
			]`,
			want: "source_classes",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := semanticprofile.LoadStatusesFromEnv(func(key string) string {
				if key == semanticprofile.EnvProviderProfilesJSON {
					return tt.raw
				}
				return ""
			})
			if err == nil {
				t.Fatal("LoadStatusesFromEnv() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadStatusesFromEnv() error = %q, want %q context", err, tt.want)
			}
		})
	}
}
