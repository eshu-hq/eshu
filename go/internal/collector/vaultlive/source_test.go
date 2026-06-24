// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vaultlive

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// fakeVaultClient is a metadata-only test double returning canned metadata for
// each Vault fact family.
type fakeVaultClient struct {
	authMounts   []AuthMount
	authRoles    []AuthRole
	aclPolicies  []ACLPolicy
	entities     []IdentityEntity
	aliases      []IdentityAlias
	kvMetadata   []KVMetadata
	engineMounts []SecretEngineMount
}

func (c *fakeVaultClient) ListAuthMounts(context.Context) ([]AuthMount, error) {
	return append([]AuthMount(nil), c.authMounts...), nil
}

func (c *fakeVaultClient) ListAuthRoles(context.Context) ([]AuthRole, error) {
	return append([]AuthRole(nil), c.authRoles...), nil
}

func (c *fakeVaultClient) ListACLPolicies(context.Context) ([]ACLPolicy, error) {
	return append([]ACLPolicy(nil), c.aclPolicies...), nil
}

func (c *fakeVaultClient) ListIdentityEntities(context.Context) ([]IdentityEntity, error) {
	return append([]IdentityEntity(nil), c.entities...), nil
}

func (c *fakeVaultClient) ListIdentityAliases(context.Context) ([]IdentityAlias, error) {
	return append([]IdentityAlias(nil), c.aliases...), nil
}

func (c *fakeVaultClient) ListKVMetadata(context.Context) ([]KVMetadata, error) {
	return append([]KVMetadata(nil), c.kvMetadata...), nil
}

func (c *fakeVaultClient) ListSecretEngineMounts(context.Context) ([]SecretEngineMount, error) {
	return append([]SecretEngineMount(nil), c.engineMounts...), nil
}

func testTarget() VaultTarget {
	return VaultTarget{
		VaultClusterID: "vault-prod",
		Namespace:      "admin",
		ScopeID:        "scope-vault-prod",
		GenerationID:   "gen-1",
		FencingToken:   7,
		ObservedAt:     time.Unix(1700000000, 0).UTC(),
		SourceURI:      "https://vault.example.com/v1/sys/auth",
	}
}

// TestCollectEmitsAuthMountFact proves the source maps one Vault auth mount to a
// vault_auth_mount source fact. It is the first red test of the Vault lane.
func TestCollectEmitsAuthMountFact(t *testing.T) {
	t.Parallel()

	client := &fakeVaultClient{
		authMounts: []AuthMount{
			{
				Path:                   "kubernetes/",
				Accessor:               "auth_kubernetes_abc",
				Method:                 "kubernetes",
				Local:                  false,
				DefaultLeaseTTLSeconds: 3600,
				MaxLeaseTTLSeconds:     7200,
			},
		},
	}
	source := Source{CollectorInstanceID: "vaultlive-1", RedactionKey: testRedactionKey(t)}

	envelopes, err := source.Collect(context.Background(), testTarget(), client)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if got, want := len(envelopes), 1; got != want {
		t.Fatalf("len(envelopes) = %d, want %d", got, want)
	}
	if got, want := envelopes[0].FactKind, facts.VaultAuthMountFactKind; got != want {
		t.Fatalf("FactKind = %q, want %q", got, want)
	}
	if got, want := envelopes[0].ScopeID, "scope-vault-prod"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
}

// TestCollectEmitsAllSevenFactFamilies proves Source.Collect maps every Vault
// metadata fact family through the secretsiam envelope builders.
func TestCollectEmitsAllSevenFactFamilies(t *testing.T) {
	t.Parallel()

	client := &fakeVaultClient{
		authMounts: []AuthMount{{Path: "kubernetes/", Accessor: "auth_k8s_a", Method: "kubernetes"}},
		authRoles: []AuthRole{{
			MountPath: "kubernetes/", RoleName: "payments-api", Method: "kubernetes",
			KubernetesClusterID:           "cluster-prod",
			BoundServiceAccountNames:      []string{"payments"},
			BoundServiceAccountNamespaces: []string{"prod"},
			TokenPolicyNames:              []string{"payments-read"},
		}},
		aclPolicies: []ACLPolicy{{
			PolicyName: "payments-read", PolicyHash: "sha256:pol",
			Rules: []ACLRule{{Path: "secret/metadata/payments", Capabilities: []string{"read", "list"}}},
		}},
		entities:     []IdentityEntity{{EntityID: "ent-1", EntityName: "payments", AliasCount: 1}},
		aliases:      []IdentityAlias{{AliasID: "alias-1", EntityID: "ent-1", MountPath: "kubernetes/", MountAccessor: "auth_k8s_a", AliasName: "payments"}},
		kvMetadata:   []KVMetadata{{MountPath: "secret/", Path: "payments/db", CurrentVersion: 3, MaxVersions: 10, CustomMetadataKeys: []string{"owner"}}},
		engineMounts: []SecretEngineMount{{MountPath: "secret/", MountAccessor: "kv_a", MountType: "kv-v2", KVVersion: "2"}},
	}
	source := Source{CollectorInstanceID: "vaultlive-1", RedactionKey: testRedactionKey(t)}

	envelopes, err := source.Collect(context.Background(), testTarget(), client)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	got := map[string]int{}
	for _, env := range envelopes {
		got[env.FactKind]++
	}
	for _, kind := range []string{
		facts.VaultAuthMountFactKind,
		facts.VaultAuthRoleFactKind,
		facts.VaultACLPolicyFactKind,
		facts.VaultIdentityEntityFactKind,
		facts.VaultIdentityAliasFactKind,
		facts.VaultKVMetadataFactKind,
		facts.VaultSecretEngineMountFactKind,
	} {
		if got[kind] != 1 {
			t.Fatalf("fact kind %q emitted %d times, want 1 (got: %v)", kind, got[kind], got)
		}
	}
}

// TestCollectNeverEmitsRawSecretMaterial proves the redaction boundary across
// every Vault fact family: raw mount paths, accessors, role names, policy
// names, ACL rule paths, identity ids/names, KV paths, custom-metadata key
// names, bound ServiceAccount names/namespaces, and the Vault namespace must
// never appear verbatim in any emitted payload, because the envelope builders
// fingerprint them. It scans nested values (slices, maps) so a leak in a
// fingerprint list or the ACL rules slice cannot slip past a top-level scan.
func TestCollectNeverEmitsRawSecretMaterial(t *testing.T) {
	t.Parallel()

	// Each sentinel is a raw value the builders must fingerprint. None is a
	// low-cardinality enum (auth_method, mount_type, kv_version, capabilities),
	// which are intentionally cleartext and excluded here.
	sentinels := []string{
		"RAWNAMESPACE-team", "RAWMOUNT-kubernetes/", "RAWACCESSOR-k8s",
		"RAWROLE-payments", "RAWSA-payments", "RAWNS-prod", "RAWPOLICY-read",
		"RAWACLNAME-payments", "RAWACLPATH-secret/metadata/payments",
		"RAWENTID-1", "RAWENTNAME-payments", "RAWALIASID-1", "RAWALIASNAME-payments",
		"RAWKVMOUNT-secret/", "RAWKVPATH-payments/db", "RAWCMK-owner",
		"RAWENGMOUNT-secret/", "RAWENGACC-kv",
	}

	client := &fakeVaultClient{
		authMounts: []AuthMount{{Path: "RAWMOUNT-kubernetes/", Accessor: "RAWACCESSOR-k8s", Method: "kubernetes"}},
		authRoles: []AuthRole{{
			MountPath: "RAWMOUNT-kubernetes/", RoleName: "RAWROLE-payments", Method: "kubernetes",
			KubernetesClusterID:           "cluster-prod",
			BoundServiceAccountNames:      []string{"RAWSA-payments"},
			BoundServiceAccountNamespaces: []string{"RAWNS-prod"},
			TokenPolicyNames:              []string{"RAWPOLICY-read"},
		}},
		aclPolicies: []ACLPolicy{{
			PolicyName: "RAWACLNAME-payments", PolicyHash: "sha256:abc",
			Rules: []ACLRule{{Path: "RAWACLPATH-secret/metadata/payments", Capabilities: []string{"read"}}},
		}},
		entities: []IdentityEntity{{EntityID: "RAWENTID-1", EntityName: "RAWENTNAME-payments", AliasCount: 1}},
		aliases: []IdentityAlias{{
			AliasID: "RAWALIASID-1", EntityID: "RAWENTID-1", MountPath: "RAWMOUNT-kubernetes/",
			MountAccessor: "RAWACCESSOR-k8s", AliasName: "RAWALIASNAME-payments",
		}},
		kvMetadata: []KVMetadata{{
			MountPath: "RAWKVMOUNT-secret/", Path: "RAWKVPATH-payments/db",
			CurrentVersion: 3, MaxVersions: 10, CustomMetadataKeys: []string{"RAWCMK-owner"},
		}},
		engineMounts: []SecretEngineMount{{MountPath: "RAWENGMOUNT-secret/", MountAccessor: "RAWENGACC-kv", MountType: "kv-v2", KVVersion: "2"}},
	}
	source := Source{CollectorInstanceID: "vaultlive-1", RedactionKey: testRedactionKey(t)}

	target := testTarget()
	target.Namespace = "RAWNAMESPACE-team"

	envelopes, err := source.Collect(context.Background(), target, client)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatal("Collect() emitted no envelopes; cannot assert redaction")
	}
	for _, env := range envelopes {
		for key, val := range env.Payload {
			for _, sentinel := range sentinels {
				if payloadContains(val, sentinel) {
					t.Fatalf("fact %q payload[%q] leaks raw Vault material %q", env.FactKind, key, sentinel)
				}
			}
		}
	}
}

// payloadContains reports whether needle appears verbatim in any string nested
// anywhere within v (strings, slices, and maps), so a leak in a fingerprint
// list or the ACL rules slice-of-maps cannot evade a top-level-only scan.
func payloadContains(v any, needle string) bool {
	switch typed := v.(type) {
	case string:
		return strings.Contains(typed, needle)
	case []string:
		for _, s := range typed {
			if strings.Contains(s, needle) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if payloadContains(item, needle) {
				return true
			}
		}
	case []map[string]any:
		for _, item := range typed {
			if payloadContains(item, needle) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if payloadContains(item, needle) {
				return true
			}
		}
	}
	return false
}

// TestCollectSanitizesCredentialBearingSourceURI proves a Vault endpoint URL
// carrying basic-auth userinfo or a token query parameter is stripped to
// scheme://host/path before it can reach a fact's SourceRef.
func TestCollectSanitizesCredentialBearingSourceURI(t *testing.T) {
	t.Parallel()

	target := testTarget()
	target.SourceURI = "https://vaultuser:s3cr3t-token@vault.example.com:8200/v1/sys/auth?token=abcd1234#frag"
	client := &fakeVaultClient{authMounts: []AuthMount{{Path: "kubernetes/", Accessor: "acc", Method: "kubernetes"}}}

	envelopes, err := Source{CollectorInstanceID: "vaultlive-1", RedactionKey: testRedactionKey(t)}.Collect(context.Background(), target, client)
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatal("Collect() emitted no envelopes")
	}
	for _, leak := range []string{"s3cr3t-token", "vaultuser", "token=abcd1234", "abcd1234"} {
		for _, env := range envelopes {
			if strings.Contains(env.SourceRef.SourceURI, leak) {
				t.Fatalf("SourceRef.SourceURI %q leaks credential material %q", env.SourceRef.SourceURI, leak)
			}
		}
	}
	if got, want := envelopes[0].SourceRef.SourceURI, "https://vault.example.com:8200/v1/sys/auth"; got != want {
		t.Fatalf("sanitized SourceURI = %q, want %q", got, want)
	}
}

// TestClientSurfaceIsMetadataOnly proves, structurally, that the Vault Client
// seam exposes no operation that reads a secret value. This guards the
// metadata-only contract against future methods that would read KV /data,
// tokens, or AppRole secret_id.
func TestClientSurfaceIsMetadataOnly(t *testing.T) {
	t.Parallel()

	forbidden := []string{"getsecret", "readsecret", "readdata", "kvdata", "secretid", "token", "unwrap"}
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	for i := 0; i < clientType.NumMethod(); i++ {
		name := strings.ToLower(clientType.Method(i).Name)
		for _, bad := range forbidden {
			if strings.Contains(name, bad) {
				t.Fatalf("Client exposes value-reading method %q (contains %q); the lane must stay metadata-only", clientType.Method(i).Name, bad)
			}
		}
	}
}

// aclFailClient errors only on ListACLPolicies to exercise per-family resilience.
type aclFailClient struct{ *fakeVaultClient }

func (c aclFailClient) ListACLPolicies(context.Context) ([]ACLPolicy, error) {
	return nil, errors.New("permission denied reading /sys/policies/acl at https://vault.example.com")
}

func TestCollectIsResilientToOneFamilyFailure(t *testing.T) {
	t.Parallel()

	base := &fakeVaultClient{authMounts: []AuthMount{{Path: "kubernetes/", Accessor: "acc", Method: "kubernetes"}}}
	envelopes, err := Source{CollectorInstanceID: "vaultlive-1", RedactionKey: testRedactionKey(t)}.Collect(context.Background(), testTarget(), aclFailClient{base})
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil (one family failure must not fail the generation)", err)
	}

	var sawAuthMount, sawWarning bool
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.VaultAuthMountFactKind:
			sawAuthMount = true
		case facts.SecretsIAMCoverageWarningFactKind:
			sawWarning = true
			if got := payloadStringValue(env.Payload, "resource_scope"); got != vaultFamilyACLPolicies {
				t.Fatalf("coverage warning resource_scope = %q, want %q", got, vaultFamilyACLPolicies)
			}
		}
		// The raw Vault error (path + address) must never reach the fact.
		for key, val := range env.Payload {
			if s, ok := val.(string); ok && (strings.Contains(s, "/sys/policies/acl") || strings.Contains(s, "vault.example.com")) {
				t.Fatalf("payload[%q] = %q leaks the raw Vault error", key, s)
			}
		}
	}
	if !sawAuthMount {
		t.Fatal("other families were not collected after the ACL family failed")
	}
	if !sawWarning {
		t.Fatal("no secrets_iam_coverage_warning emitted for the failed family")
	}
}

type allFailClient struct{ *fakeVaultClient }

func (c allFailClient) ListAuthMounts(ctx context.Context) ([]AuthMount, error) {
	return nil, ctx.Err()
}

func TestCollectContextCancellationIsFatal(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Source{CollectorInstanceID: "vaultlive-1", RedactionKey: testRedactionKey(t)}.Collect(ctx, testTarget(), allFailClient{&fakeVaultClient{}})
	if err == nil {
		t.Fatal("Collect() error = nil, want fatal on context cancellation")
	}
}
