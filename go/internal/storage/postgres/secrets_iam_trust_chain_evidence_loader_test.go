package postgres

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestFactStoreLoadSecretsIAMTrustChainEvidenceExpandsActiveAnchors(t *testing.T) {
	t.Parallel()

	serviceAccountKey := "sha256:service-account"
	roleARN := "arn:aws:iam::123456789012:role/payments-api"
	policyKey := "sha256:vault-policy"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"k8s-sa",
					"k8s-scope",
					"k8s-gen",
					facts.KubernetesServiceAccountFactKind,
					`{"service_account_join_key":"sha256:service-account"}`,
				),
			}},
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"k8s-irsa",
					"k8s-scope",
					"k8s-gen",
					facts.EKSIRSAAnnotationFactKind,
					`{"service_account_join_key":"sha256:service-account","role_arn":"arn:aws:iam::123456789012:role/payments-api"}`,
				),
				secretsIAMTrustChainFactRow(
					"vault-role",
					"vault-scope",
					"vault-gen",
					facts.VaultAuthRoleFactKind,
					`{"bound_service_account_join_keys":["sha256:service-account"],"token_policy_join_keys":["sha256:vault-policy"]}`,
				),
			}},
			{rows: nil},
		},
	}
	store := NewFactStore(db)

	envelopes, stats, err := store.LoadSecretsIAMTrustChainEvidence(context.Background(), reducer.Intent{
		ScopeID:      "k8s-scope",
		GenerationID: "k8s-gen",
		Domain:       reducer.DomainSecretsIAMTrustChain,
	})
	if err != nil {
		t.Fatalf("LoadSecretsIAMTrustChainEvidence() error = %v, want nil", err)
	}
	if got, want := stats.SeedFactCount, 1; got != want {
		t.Fatalf("SeedFactCount = %d, want %d", got, want)
	}
	if got, want := stats.LoadedFactCount, 3; got != want {
		t.Fatalf("LoadedFactCount = %d, want %d", got, want)
	}
	if got, want := len(envelopes), 3; got != want {
		t.Fatalf("len(envelopes) = %d, want %d", got, want)
	}
	if got, want := len(db.queries), 3; got != want {
		t.Fatalf("QueryContext calls = %d, want %d", got, want)
	}
	activeQuery := db.queries[1].query
	for _, want := range []string{
		"scope.active_generation_id = fact.generation_id",
		"generation.status = 'active'",
		"fact.payload->>'service_account_join_key' = ANY($2::text[])",
		"fact.payload->'bound_service_account_join_keys' ?| $2::text[]",
		"fact.payload->>'role_arn' = ANY($3::text[])",
		"fact.payload->'token_policy_join_keys' ?| $5::text[]",
	} {
		if !strings.Contains(activeQuery, want) {
			t.Fatalf("active query missing %q:\n%s", want, activeQuery)
		}
	}
	if !slices.Contains(stringsArg(t, db.queries[1].args[1]), serviceAccountKey) {
		t.Fatalf("first active expansion missing service account anchor: %#v", db.queries[1].args[1])
	}
	if !slices.Contains(stringsArg(t, db.queries[2].args[2]), roleARN) {
		t.Fatalf("second active expansion missing role ARN anchor: %#v", db.queries[2].args[2])
	}
	if !slices.Contains(stringsArg(t, db.queries[2].args[4]), policyKey) {
		t.Fatalf("second active expansion missing policy anchor: %#v", db.queries[2].args[4])
	}
}

func TestFactStoreLoadSecretsIAMTrustChainEvidenceMarksTruncatedAtExpansionLimit(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"k8s-sa",
					"k8s-scope",
					"k8s-gen",
					facts.KubernetesServiceAccountFactKind,
					`{"service_account_join_key":"sha256:service-account"}`,
				),
			}},
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"k8s-irsa",
					"k8s-scope",
					"k8s-gen",
					facts.EKSIRSAAnnotationFactKind,
					`{"service_account_join_key":"sha256:service-account","role_arn":"arn:aws:iam::123456789012:role/payments-api"}`,
				),
			}},
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"vault-role",
					"vault-scope",
					"vault-gen",
					facts.VaultAuthRoleFactKind,
					`{"bound_service_account_join_keys":["sha256:service-account"],"token_policy_join_keys":["sha256:vault-policy"]}`,
				),
			}},
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"vault-policy",
					"vault-scope",
					"vault-gen",
					facts.VaultACLPolicyFactKind,
					`{"policy_join_key":"sha256:vault-policy","rules":[{"path_fingerprint":"sha256:kv-path"}]}`,
				),
			}},
			{rows: [][]any{
				secretsIAMTrustChainFactRow(
					"vault-kv",
					"vault-scope",
					"vault-gen",
					facts.VaultKVMetadataFactKind,
					`{"kv_path_fingerprint":"sha256:kv-path"}`,
				),
			}},
		},
	}
	store := NewFactStore(db)

	_, stats, err := store.LoadSecretsIAMTrustChainEvidence(context.Background(), reducer.Intent{
		ScopeID:      "k8s-scope",
		GenerationID: "k8s-gen",
		Domain:       reducer.DomainSecretsIAMTrustChain,
	})
	if err != nil {
		t.Fatalf("LoadSecretsIAMTrustChainEvidence() error = %v, want nil", err)
	}
	if !stats.Truncated {
		t.Fatal("Truncated = false, want true after expansion limit adds new anchors")
	}
	if got, want := len(db.queries), secretsIAMTrustChainMaxExpansionPasses+1; got != want {
		t.Fatalf("QueryContext calls = %d, want %d", got, want)
	}
}

func secretsIAMTrustChainFactRow(
	factID string,
	scopeID string,
	generationID string,
	factKind string,
	payload string,
) []any {
	version, _ := facts.SecretsIAMSchemaVersion(factKind)
	return []any{
		factID,
		scopeID,
		generationID,
		factKind,
		factKind + ":" + factID,
		version,
		"secrets_iam_posture",
		int64(0),
		"reported",
		"secrets_iam_posture",
		factID,
		"",
		factID,
		time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC),
		false,
		[]byte(payload),
	}
}

func stringsArg(t *testing.T, arg any) []string {
	t.Helper()
	values, ok := arg.([]string)
	if !ok {
		t.Fatalf("arg type = %T, want []string", arg)
	}
	return values
}
