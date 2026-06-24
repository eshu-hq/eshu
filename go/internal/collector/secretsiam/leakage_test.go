// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// This file is the unified, cross-family secret-leakage guard for the
// secrets/IAM posture collectors (issue #25, #1348). Every AWS IAM, Kubernetes,
// and Vault source fact funnels through the envelope builders in this package,
// so asserting the invariants here covers all three lanes at their single
// chokepoint and stays correct as new builders are added.
//
// The guarantee is structural: the observation types carry no field for a
// secret value, token, JWT, AppRole secret_id, private key, raw policy body, or
// KV data, so a fact cannot leak what it has no field to hold. These tests lock
// that — (1) every builder stamps the redaction policy version, and (2) no
// payload key names a raw secret value — plus positive canary checks proving the
// fields that must be fingerprinted are.
//
// Scope: this guard covers the FACT payloads, which is where #25 forbids secret
// material. The envelope builders are pure functions that emit no logs or
// metrics, so there is nothing to scan there. Log/metric canary scanning
// belongs at the source-collector layer (awscloud, kuberneteslive, vaultlive),
// which is where logging and metric emission actually happen; that layer's
// own tests own that assertion.

func leakAWSContext() EnvelopeContext {
	return EnvelopeContext{
		AccountID: "111111111111", Region: "us-east-1",
		ScopeID: "scope-aws", GenerationID: "gen-1", CollectorInstanceID: "ci-1",
		FencingToken: 1, ObservedAt: time.Unix(1700000000, 0).UTC(),
	}
}

func leakVaultContext() VaultContext {
	return VaultContext{
		VaultClusterID: "vault-prod", Namespace: "admin",
		ScopeID: "scope-vault", GenerationID: "gen-1", CollectorInstanceID: "ci-1",
		FencingToken: 1, ObservedAt: time.Unix(1700000000, 0).UTC(),
		RedactionKey: testSecretsIAMRedactionKey(),
	}
}

func leakGCPContext() GCPEnvelopeContext {
	return GCPEnvelopeContext{
		ProjectID: "demo-proj", LocationBucket: "global",
		ScopeID: "scope-gcp", GenerationID: "gen-1", CollectorInstanceID: "ci-1",
		FencingToken: 1, ObservedAt: time.Unix(1700000000, 0).UTC(),
	}
}

func leakK8sContext() KubernetesContext {
	return KubernetesContext{
		ClusterID: "cluster-prod",
		ScopeID:   "scope-k8s", GenerationID: "gen-1", CollectorInstanceID: "ci-1",
		FencingToken: 1, ObservedAt: time.Unix(1700000000, 0).UTC(),
	}
}

// secretsIAMBuilderCases returns one populated envelope per builder across all
// three lanes. Sensitive fields are filled so the leak assertions exercise real
// payload content.
func secretsIAMBuilderCases(t *testing.T) map[string]facts.Envelope {
	t.Helper()
	aws := leakAWSContext()
	vault := leakVaultContext()
	k8s := leakK8sContext()
	gcp := leakGCPContext()

	builders := map[string]func() (facts.Envelope, error){
		"aws_principal": func() (facts.Envelope, error) {
			return NewPrincipalEnvelope(PrincipalObservation{
				Context: aws, PrincipalARN: "arn:aws:iam::111111111111:role/app", PrincipalType: PrincipalTypeAWSRole,
				Name: "app", Path: "/", URLFingerprint: "sha256:url", CorrelationHints: []string{"hint"},
			})
		},
		"aws_trust_policy": func() (facts.Envelope, error) {
			return NewTrustPolicyEnvelope(TrustPolicyObservation{
				Context: aws, RoleARN: "arn:aws:iam::111111111111:role/app", Effect: "Allow",
				Actions: []string{"sts:AssumeRole"}, ConditionKeys: []string{"sts:ExternalId"},
				AssumePrincipals: []string{"arn:aws:iam::999999999999:root"}, WebIdentitySubjectFingerprints: []string{"sha256:sub"},
			})
		},
		"aws_permission_policy": func() (facts.Envelope, error) {
			return NewPermissionPolicyEnvelope(PermissionPolicyObservation{
				Context: aws, PrincipalARN: "arn:aws:iam::111111111111:role/app", PolicySource: PolicySourceInline,
				Effect: "Allow", Actions: []string{"s3:GetObject"}, Resources: []string{"arn:aws:s3:::bucket/*"},
				ConditionKeys: []string{"aws:SourceVpc"},
			})
		},
		"aws_policy_attachment": func() (facts.Envelope, error) {
			return NewPolicyAttachmentEnvelope(PolicyAttachmentObservation{
				Context: aws, PrincipalARN: "arn:aws:iam::111111111111:role/app",
				PolicyARN: "arn:aws:iam::aws:policy/ReadOnly", PolicyName: "ReadOnly", PolicySource: PolicySourceAttachedManaged,
			})
		},
		"aws_permission_boundary": func() (facts.Envelope, error) {
			return NewPermissionBoundaryEnvelope(PermissionBoundaryObservation{
				Context: aws, PrincipalARN: "arn:aws:iam::111111111111:role/app",
				BoundaryPolicyARN: "arn:aws:iam::111111111111:policy/Boundary", BoundaryType: "policy",
			})
		},
		"gcp_principal": func() (facts.Envelope, error) {
			return NewGCPPrincipalEnvelope(GCPPrincipalObservation{
				Context: gcp, PrincipalFingerprint: "sha256:svc", MemberClass: GCPMemberClassServiceAccount,
			})
		},
		"gcp_trust_policy": func() (facts.Envelope, error) {
			return NewGCPTrustPolicyEnvelope(GCPTrustPolicyObservation{
				Context:                               gcp,
				TargetPrincipalFingerprint:            "sha256:gcp-sa",
				TargetServiceAccountEmailDigest:       GCPServiceAccountEmailDigest("app@demo-proj.iam.gserviceaccount.com"),
				TargetServiceAccountCloudResourceUID:  "cloud-resource:gcp-sa",
				TrustedMemberFingerprint:              "sha256:gke-member",
				TrustedMemberClass:                    GCPMemberClassServiceAccount,
				Role:                                  "roles/iam.workloadIdentityUser",
				ImpersonationMode:                     GCPImpersonationModeWorkloadIdentity,
				GCPWorkloadIdentitySubjectFingerprint: GCPWorkloadIdentitySubjectFingerprint("demo-proj.svc.id.goog", "ns-canary", "ksa-canary"),
				GCPWorkloadIdentityMemberClass:        GCPWorkloadIdentityMemberClassServiceAccount,
			})
		},
		"gcp_permission_policy": func() (facts.Envelope, error) {
			return NewGCPPermissionPolicyEnvelope(GCPPermissionPolicyObservation{
				Context: gcp, PrincipalFingerprint: "sha256:svc", Role: "roles/secretmanager.secretAccessor",
				ResourceFullName:  "//secretmanager.googleapis.com/projects/demo-proj/secrets/db",
				ResourceAssetType: "secretmanager.googleapis.com/Secret", ResourceIsSecret: true,
			})
		},
		"aws_instance_profile": func() (facts.Envelope, error) {
			return NewInstanceProfileEnvelope(InstanceProfileObservation{
				Context: aws, ProfileARN: "arn:aws:iam::111111111111:instance-profile/p", Name: "p", Path: "/",
				RoleARNs: []string{"arn:aws:iam::111111111111:role/app"},
			})
		},
		"aws_access_analyzer_finding": func() (facts.Envelope, error) {
			return NewAccessAnalyzerFindingEnvelope(AccessAnalyzerFindingObservation{
				Context: aws, FindingID: "f-1", AnalyzerARN: "arn:aws:access-analyzer:us-east-1:111111111111:analyzer/a",
				ResourceARN: "arn:aws:iam::111111111111:role/app", ResourceType: "AWS::IAM::Role", Status: "ACTIVE",
				FindingType: "ExternalAccess", ConditionKeys: []string{"aws:PrincipalOrgID"},
			})
		},
		"aws_coverage_warning": func() (facts.Envelope, error) {
			return NewCoverageWarningEnvelope(CoverageWarningObservation{
				Context: aws, WarningKind: "partial", SourceState: SourceStatePartial, ErrorClass: "throttle", Message: "m",
			})
		},
		"vault_auth_mount": func() (facts.Envelope, error) {
			return NewVaultAuthMountEnvelope(VaultAuthMountObservation{
				Context: vault, MountPath: "kubernetes/", MountAccessor: "accessor-canary", AuthMethod: VaultAuthMethodKubernetes,
			})
		},
		"vault_auth_role": func() (facts.Envelope, error) {
			return NewVaultAuthRoleEnvelope(VaultAuthRoleObservation{
				Context: vault, MountPath: "kubernetes/", RoleName: "rolename-canary", AuthMethod: VaultAuthMethodKubernetes,
				BoundServiceAccountNames: []string{"saname-canary"}, BoundServiceAccountNamespaces: []string{"sans-canary"},
				TokenPolicyNames: []string{"policyname-canary"}, TokenTTLSeconds: 3600,
			})
		},
		"vault_acl_policy": func() (facts.Envelope, error) {
			return NewVaultACLPolicyEnvelope(VaultACLPolicyObservation{
				Context: vault, PolicyName: "payments-read", PolicyHash: "sha256:pol",
				Rules: []VaultACLPolicyRuleSummary{{Path: "secret/metadata/payments", Capabilities: []string{"read"}}},
			})
		},
		"vault_identity_entity": func() (facts.Envelope, error) {
			return NewVaultIdentityEntityEnvelope(VaultIdentityEntityObservation{
				Context: vault, EntityID: "ent-1", EntityName: "payments", AliasCount: 1,
			})
		},
		"vault_identity_alias": func() (facts.Envelope, error) {
			return NewVaultIdentityAliasEnvelope(VaultIdentityAliasObservation{
				Context: vault, AliasID: "alias-1", EntityID: "ent-1", MountPath: "kubernetes/", MountAccessor: "accessor-canary", AliasName: "payments",
			})
		},
		"vault_kv_metadata": func() (facts.Envelope, error) {
			return NewVaultKVMetadataEnvelope(VaultKVMetadataObservation{
				Context: vault, MountPath: "secret/", Path: "payments/db", CurrentVersion: 3, MaxVersions: 10,
				CustomMetadataKeys: []string{"owner"},
			})
		},
		"vault_secret_engine_mount": func() (facts.Envelope, error) {
			return NewVaultSecretEngineMountEnvelope(VaultSecretEngineMountObservation{
				Context: vault, MountPath: "secret/", MountAccessor: "accessor-canary", MountType: VaultSecretEngineKVV2, KVVersion: "2",
			})
		},
		"vault_coverage_warning": func() (facts.Envelope, error) {
			// Populate Attributes to exercise the free-form map path: the Vault
			// builder fingerprints attribute keys and drops values, so even a
			// secret-named attribute key must not appear cleartext.
			return NewVaultCoverageWarningEnvelope(VaultCoverageWarningObservation{
				Context: vault, WarningKind: "partial", SourceState: SourceStatePartial, ResourceScope: "auth_roles",
				ErrorClass: "throttle", Message: "rate limited",
				Attributes: map[string]any{"access_token-canary": "tokenvalue-canary"},
			})
		},
		"k8s_service_account": func() (facts.Envelope, error) {
			return NewKubernetesServiceAccountEnvelope(KubernetesServiceAccountObservation{
				Context: k8s, Namespace: "prod", Name: "payments", UID: "uid-1", AnnotationKeys: []string{"eks.amazonaws.com/role-arn"},
				AutomountToken: BoolStateTrue, ResourceVersion: "100",
			})
		},
		"k8s_token_posture": func() (facts.Envelope, error) {
			return NewKubernetesServiceAccountTokenPostureEnvelope(KubernetesServiceAccountTokenPostureObservation{
				Context: k8s, Namespace: "prod", ServiceAccountName: "payments", ServiceAccountUID: "uid-1", AutomountToken: BoolStateTrue,
			})
		},
		"k8s_rbac_role": func() (facts.Envelope, error) {
			return NewKubernetesRBACRoleEnvelope(KubernetesRBACRoleObservation{
				Context: k8s, RoleKind: "Role", Namespace: "prod", Name: "reader", UID: "uid-r", ResourceVersion: "1",
				Rules: []KubernetesRBACRuleSummary{{Verbs: []string{"get"}, APIGroups: []string{""}, Resources: []string{"secrets"}}},
			})
		},
		"k8s_rbac_binding": func() (facts.Envelope, error) {
			return NewKubernetesRBACBindingEnvelope(KubernetesRBACBindingObservation{
				Context: k8s, BindingKind: "RoleBinding", Namespace: "prod", Name: "bind", UID: "uid-b", ResourceVersion: "1",
				RoleRefKind: "Role", RoleRefName: "reader", SubjectCount: 1,
				Subjects: []KubernetesRBACSubject{{Kind: "ServiceAccount", Namespace: "prod", Name: "payments"}},
			})
		},
		"k8s_workload_identity_use": func() (facts.Envelope, error) {
			return NewKubernetesWorkloadIdentityUseEnvelope(KubernetesWorkloadIdentityUseObservation{
				Context: k8s, WorkloadObjectID: "obj-1", WorkloadKind: "Deployment", Namespace: "prod",
				ServiceAccountName: "payments", ServiceAccountUID: "uid-1", ProjectedServiceAccountToken: true,
			})
		},
		"k8s_gcp_workload_identity_binding": func() (facts.Envelope, error) {
			return NewKubernetesGCPWorkloadIdentityBindingEnvelope(KubernetesGCPWorkloadIdentityBindingObservation{
				Context:                k8s,
				Namespace:              "ns-canary",
				ServiceAccountName:     "ksa-canary",
				ServiceAccountUID:      "uid-1",
				GCPServiceAccountEmail: "app@demo-proj.iam.gserviceaccount.com",
				GCPWorkloadPool:        "demo-proj.svc.id.goog",
				AnnotationPresent:      true,
			})
		},
		"eks_irsa_annotation": func() (facts.Envelope, error) {
			return NewEKSIRSAAnnotationEnvelope(EKSIRSAAnnotationObservation{
				Context: k8s, Namespace: "prod", ServiceAccountName: "payments", ServiceAccountUID: "uid-1",
				RoleARN: "arn:aws:iam::111111111111:role/app", AnnotationPresent: true,
			})
		},
		"eks_pod_identity_association": func() (facts.Envelope, error) {
			return NewEKSPodIdentityAssociationEnvelope(EKSPodIdentityAssociationObservation{
				Context: k8s, AssociationID: "assoc-1", ClusterName: "cluster-prod", Namespace: "prod",
				ServiceAccountName: "payments", RoleARN: "arn:aws:iam::111111111111:role/app",
			})
		},
		"k8s_coverage_warning": func() (facts.Envelope, error) {
			return NewKubernetesCoverageWarningEnvelope(KubernetesCoverageWarningObservation{
				Context: k8s, WarningKind: "partial", SourceState: SourceStatePartial, ErrorClass: "forbidden", Message: "m",
			})
		},
	}

	out := map[string]facts.Envelope{}
	for name, fn := range builders {
		env, err := fn()
		if err != nil {
			t.Fatalf("build %s: %v", name, err)
		}
		out[name] = env
	}
	return out
}

// secretValueKeyTokens are substrings in a payload key that would indicate a raw
// secret value, token, or credential — content the secrets/IAM facts must never
// carry. A payload key containing one of these (and not in the posture allow-list
// below) fails the test.
var secretValueKeyTokens = []string{
	"secret_id", "secretid", "client_secret", "clientsecret", "private_key", "privatekey",
	"private_key_pem", "pem", "password", "passwd", "passphrase",
	"secret_access_key", "access_key_id", "accesskey", "access_token", "refresh_token",
	"auth_token", "session_token", "sessiontoken", "token_value", "api_key", "apikey",
	"bearer", "jwt", "credential", "raw_policy", "policy_document", "policy_body",
	"kv_data", "secret_value", "secret_string", "secret_data", "client_id_secret",
}

// posturePayloadKeyAllowList are payload keys that legitimately contain a
// secret-adjacent word but carry only posture/config metadata, never a value
// (for example whether a token is automounted, or token TTL/policy-name config).
var posturePayloadKeyAllowList = map[string]struct{}{
	"automount_token":                 {},
	"projected_service_account_token": {},
	"token_ttl_seconds":               {},
	"token_policy_names":              {},
	"token_policy_count":              {},
	"token_policy_join_keys":          {},
}

// TestBuildersNeverNameASecretValue proves no secrets/IAM builder emits a
// payload key that names a raw secret value, and that every builder stamps the
// redaction policy version. This is the structural guarantee that a fact cannot
// leak a secret it has no field to hold.
func TestBuildersNeverNameASecretValue(t *testing.T) {
	t.Parallel()

	for name, env := range secretsIAMBuilderCases(t) {
		name, env := name, env
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := env.Payload["redaction_policy_version"]; got != RedactionPolicyVersion {
				t.Fatalf("%s: redaction_policy_version = %v, want %q", name, got, RedactionPolicyVersion)
			}
			for _, key := range collectPayloadKeys(env.Payload) {
				if _, ok := posturePayloadKeyAllowList[key]; ok {
					continue
				}
				lower := strings.ToLower(key)
				for _, token := range secretValueKeyTokens {
					if strings.Contains(lower, token) {
						t.Fatalf("%s: payload key %q names a secret value (matched %q)", name, key, token)
					}
				}
			}
		})
	}
}

// TestBuildersFingerprintRawSensitiveValues proves the fields the contract says
// must be fingerprinted do not echo their raw input — a positive complement to
// the structural key-name check, covering the highest-risk fields per lane.
func TestBuildersFingerprintRawSensitiveValues(t *testing.T) {
	t.Parallel()

	cases := secretsIAMBuilderCases(t)

	// assertAbsent fails if raw appears in any payload value (recursively),
	// proving the field was fingerprinted rather than echoed cleartext.
	assertAbsent := func(t *testing.T, name string, env facts.Envelope, raw string) {
		t.Helper()
		for key, val := range env.Payload {
			if leakValueContains(val, raw) {
				t.Fatalf("%s: payload[%q] leaks raw value %q (should be fingerprinted)", name, key, raw)
			}
		}
	}

	// The Vault builders fingerprint their raw inputs in-package (unlike the AWS
	// builders, which intentionally retain ARNs/names for graph joins), so the
	// raw inputs must never survive cleartext. Canaries are distinctive and
	// non-hex so they cannot collide with cleartext enums or sha256 fingerprint
	// hex.
	checks := map[string][]string{
		// policy name, rule path.
		"vault_acl_policy": {"payments-read", "secret/metadata/payments"},
		// mount path, key path, custom-metadata key name.
		"vault_kv_metadata": {"secret/", "payments/db", "owner"},
		// role name and bound ServiceAccount selectors.
		"vault_auth_role": {"rolename-canary", "saname-canary", "sans-canary", "policyname-canary"},
		// mount accessor is fingerprinted (mount_accessor_fingerprint).
		"vault_auth_mount":          {"accessor-canary"},
		"vault_identity_alias":      {"accessor-canary"},
		"vault_secret_engine_mount": {"accessor-canary"},
		// coverage-warning attribute keys are fingerprinted and values dropped,
		// so neither the secret-shaped key nor its value may survive cleartext.
		"vault_coverage_warning": {"access_token-canary", "tokenvalue-canary"},
	}
	for name, raws := range checks {
		for _, raw := range raws {
			assertAbsent(t, name, cases[name], raw)
		}
	}
}

// TestEveryEnvelopeBuilderIsCovered parses the package source and fails if the
// leakage guard does not call every exported New<Kind>Envelope function (or
// calls one that no longer exists). It compares identifier SETS, not counts, so
// a builder covered twice cannot mask another builder being missing — the
// blind-spot the meta-guard exists to prevent.
func TestEveryEnvelopeBuilderIsCovered(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	exported := map[string]bool{} // declared in non-test package files
	covered := map[string]bool{}  // actually called by this leak guard
	isBuilderName := func(s string) bool {
		return strings.HasPrefix(s, "New") && strings.HasSuffix(s, "Envelope")
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if !strings.HasSuffix(name, "_test.go") {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if ok && fn.Recv == nil && fn.Name.IsExported() && isBuilderName(fn.Name.Name) {
					exported[fn.Name.Name] = true
				}
			}
			continue
		}
		if name == "leakage_test.go" {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if id, ok := call.Fun.(*ast.Ident); ok && isBuilderName(id.Name) {
					covered[id.Name] = true
				}
				return true
			})
		}
	}

	for builder := range exported {
		if !covered[builder] {
			t.Errorf("leakage guard never calls exported builder %s; add a case", builder)
		}
	}
	for builder := range covered {
		if !exported[builder] {
			t.Errorf("leakage guard calls %s which is not an exported package builder", builder)
		}
	}
}

// collectPayloadKeys returns every map key nested anywhere within the payload.
func collectPayloadKeys(payload map[string]any) []string {
	var keys []string
	var walk func(v any)
	walk = func(v any) {
		switch typed := v.(type) {
		case map[string]any:
			for k, val := range typed {
				keys = append(keys, k)
				walk(val)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case []map[string]any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(payload)
	return keys
}

// leakValueContains reports whether needle appears in any string nested
// anywhere within v.
func leakValueContains(v any, needle string) bool {
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
			if leakValueContains(item, needle) {
				return true
			}
		}
	case []map[string]any:
		for _, item := range typed {
			if leakValueContains(item, needle) {
				return true
			}
		}
	case map[string]any:
		for _, item := range typed {
			if leakValueContains(item, needle) {
				return true
			}
		}
	}
	return false
}
