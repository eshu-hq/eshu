// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestNewKubernetesServiceAccountEnvelopeRedactsIdentityNames(t *testing.T) {
	ctx := testKubernetesContext()
	env, err := NewKubernetesServiceAccountEnvelope(KubernetesServiceAccountObservation{
		Context:                 ctx,
		Namespace:               "payments",
		Name:                    "checkout-sa",
		UID:                     "uid-sa-1",
		AnnotationKeys:          []string{"eks.amazonaws.com/role-arn", "vault.hashicorp.com/agent-inject"},
		AutomountToken:          BoolStateFalse,
		SecretRefCount:          2,
		ImagePullSecretRefCount: 1,
		ResourceVersion:         "rv-12345",
	})
	if err != nil {
		t.Fatalf("NewKubernetesServiceAccountEnvelope() error = %v", err)
	}
	assertFact(t, env, facts.KubernetesServiceAccountFactKind)
	assertPayloadString(t, env.Payload, "provider", ProviderKubernetes)
	assertPayloadString(t, env.Payload, "cluster_id", ctx.ClusterID)
	assertPayloadString(t, env.Payload, "automount_token", BoolStateFalse)
	if payloadValueContains(env.Payload, "payments") || payloadValueContains(env.Payload, "checkout-sa") {
		t.Fatalf("service account payload leaked raw namespace or service account name: %#v", env.Payload)
	}
	if payloadValueContains(env.Payload, "rv-12345") {
		t.Fatalf("service account payload leaked raw resource version: %#v", env.Payload)
	}
	if env.Payload["service_account_join_key"] == "" {
		t.Fatalf("service_account_join_key is empty")
	}
}

func TestNewKubernetesRBACBindingEnvelopeDistinguishesBindingScope(t *testing.T) {
	ctx := testKubernetesContext()
	namespaced, err := NewKubernetesRBACBindingEnvelope(KubernetesRBACBindingObservation{
		Context:      ctx,
		BindingKind:  BindingKindRoleBinding,
		Namespace:    "payments",
		Name:         "read-secrets",
		UID:          "uid-rb-1",
		RoleRefKind:  RBACRoleKindRole,
		RoleRefName:  "secret-reader",
		SubjectCount: 1,
		Subjects: []KubernetesRBACSubject{{
			Kind:      "ServiceAccount",
			Namespace: "payments",
			Name:      "checkout-sa",
		}},
	})
	if err != nil {
		t.Fatalf("NewKubernetesRBACBindingEnvelope(RoleBinding) error = %v", err)
	}
	assertFact(t, namespaced, facts.KubernetesRBACBindingFactKind)
	assertPayloadString(t, namespaced.Payload, "binding_scope", BindingScopeNamespace)
	if payloadValueContains(namespaced.Payload, "payments") ||
		payloadValueContains(namespaced.Payload, "checkout-sa") ||
		payloadValueContains(namespaced.Payload, "secret-reader") {
		t.Fatalf("RoleBinding payload leaked raw RBAC identity names: %#v", namespaced.Payload)
	}

	clusterWide, err := NewKubernetesRBACBindingEnvelope(KubernetesRBACBindingObservation{
		Context:      ctx,
		BindingKind:  BindingKindClusterRoleBinding,
		Name:         "cluster-admin-read",
		UID:          "uid-crb-1",
		RoleRefKind:  RBACRoleKindClusterRole,
		RoleRefName:  "cluster-admin",
		SubjectCount: 1,
		Subjects: []KubernetesRBACSubject{{
			Kind: "Group",
			Name: "platform-admins",
		}},
	})
	if err != nil {
		t.Fatalf("NewKubernetesRBACBindingEnvelope(ClusterRoleBinding) error = %v", err)
	}
	assertFact(t, clusterWide, facts.KubernetesRBACBindingFactKind)
	assertPayloadString(t, clusterWide.Payload, "binding_scope", BindingScopeCluster)
	if payloadValueContains(clusterWide.Payload, "platform-admins") ||
		payloadValueContains(clusterWide.Payload, "cluster-admin") {
		t.Fatalf("ClusterRoleBinding payload leaked raw RBAC identity names: %#v", clusterWide.Payload)
	}
}

func TestNewKubernetesRBACBindingEnvelopeUsesClusterRoleJoinScope(t *testing.T) {
	ctx := testKubernetesContext()
	clusterRole, err := NewKubernetesRBACRoleEnvelope(KubernetesRBACRoleObservation{
		Context:  ctx,
		RoleKind: RBACRoleKindClusterRole,
		Name:     "secret-reader",
		UID:      "uid-cluster-role",
	})
	if err != nil {
		t.Fatalf("NewKubernetesRBACRoleEnvelope(ClusterRole) error = %v", err)
	}
	binding, err := NewKubernetesRBACBindingEnvelope(KubernetesRBACBindingObservation{
		Context:      ctx,
		BindingKind:  BindingKindRoleBinding,
		Namespace:    "payments",
		Name:         "read-secrets",
		UID:          "uid-rb",
		RoleRefKind:  RBACRoleKindClusterRole,
		RoleRefName:  "secret-reader",
		SubjectCount: 1,
	})
	if err != nil {
		t.Fatalf("NewKubernetesRBACBindingEnvelope(RoleBinding to ClusterRole) error = %v", err)
	}
	if binding.Payload["role_ref_join_key"] != clusterRole.Payload["role_join_key"] {
		t.Fatalf("RoleBinding ClusterRole ref join key = %q, want ClusterRole join key %q",
			binding.Payload["role_ref_join_key"], clusterRole.Payload["role_join_key"])
	}
}

func TestNewKubernetesRBACRoleEnvelopeRejectsMalformedIdentity(t *testing.T) {
	ctx := testKubernetesContext()
	tests := []struct {
		name        string
		observation KubernetesRBACRoleObservation
	}{
		{
			name: "unknown role kind",
			observation: KubernetesRBACRoleObservation{
				Context:   ctx,
				RoleKind:  "Secret",
				Namespace: "payments",
				Name:      "reader",
			},
		},
		{
			name: "role missing namespace",
			observation: KubernetesRBACRoleObservation{
				Context:  ctx,
				RoleKind: RBACRoleKindRole,
				Name:     "reader",
			},
		},
		{
			name: "role missing name",
			observation: KubernetesRBACRoleObservation{
				Context:   ctx,
				RoleKind:  RBACRoleKindRole,
				Namespace: "payments",
			},
		},
		{
			name: "clusterrole missing name",
			observation: KubernetesRBACRoleObservation{
				Context:  ctx,
				RoleKind: RBACRoleKindClusterRole,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewKubernetesRBACRoleEnvelope(tt.observation); err == nil {
				t.Fatalf("NewKubernetesRBACRoleEnvelope() error = nil, want non-nil")
			}
		})
	}
}

func TestNewKubernetesRBACBindingEnvelopeRejectsMalformedRoleRef(t *testing.T) {
	ctx := testKubernetesContext()
	tests := []struct {
		name        string
		observation KubernetesRBACBindingObservation
	}{
		{
			name: "unknown binding kind",
			observation: KubernetesRBACBindingObservation{
				Context:      ctx,
				BindingKind:  "Binding",
				Namespace:    "payments",
				Name:         "read-secrets",
				RoleRefKind:  RBACRoleKindClusterRole,
				RoleRefName:  "secret-reader",
				SubjectCount: 1,
			},
		},
		{
			name: "rolebinding missing namespace",
			observation: KubernetesRBACBindingObservation{
				Context:      ctx,
				BindingKind:  BindingKindRoleBinding,
				Name:         "read-secrets",
				RoleRefKind:  RBACRoleKindRole,
				RoleRefName:  "secret-reader",
				SubjectCount: 1,
			},
		},
		{
			name: "binding missing name",
			observation: KubernetesRBACBindingObservation{
				Context:      ctx,
				BindingKind:  BindingKindRoleBinding,
				Namespace:    "payments",
				RoleRefKind:  RBACRoleKindRole,
				RoleRefName:  "secret-reader",
				SubjectCount: 1,
			},
		},
		{
			name: "unknown role ref kind",
			observation: KubernetesRBACBindingObservation{
				Context:      ctx,
				BindingKind:  BindingKindRoleBinding,
				Namespace:    "payments",
				Name:         "read-secrets",
				RoleRefKind:  "Secret",
				RoleRefName:  "secret-reader",
				SubjectCount: 1,
			},
		},
		{
			name: "role ref missing name",
			observation: KubernetesRBACBindingObservation{
				Context:      ctx,
				BindingKind:  BindingKindRoleBinding,
				Namespace:    "payments",
				Name:         "read-secrets",
				RoleRefKind:  RBACRoleKindRole,
				SubjectCount: 1,
			},
		},
		{
			name: "clusterrolebinding cannot reference role",
			observation: KubernetesRBACBindingObservation{
				Context:      ctx,
				BindingKind:  BindingKindClusterRoleBinding,
				Name:         "read-secrets",
				RoleRefKind:  RBACRoleKindRole,
				RoleRefName:  "secret-reader",
				SubjectCount: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewKubernetesRBACBindingEnvelope(tt.observation); err == nil {
				t.Fatalf("NewKubernetesRBACBindingEnvelope() error = nil, want non-nil")
			}
		})
	}
}

func TestNewKubernetesServiceAccountTokenPostureStableKeyIncludesUID(t *testing.T) {
	ctx := testKubernetesContext()
	first, err := NewKubernetesServiceAccountTokenPostureEnvelope(KubernetesServiceAccountTokenPostureObservation{
		Context:            ctx,
		Namespace:          "payments",
		ServiceAccountName: "checkout-sa",
		ServiceAccountUID:  "uid-sa-1",
	})
	if err != nil {
		t.Fatalf("NewKubernetesServiceAccountTokenPostureEnvelope(first) error = %v", err)
	}
	recreated, err := NewKubernetesServiceAccountTokenPostureEnvelope(KubernetesServiceAccountTokenPostureObservation{
		Context:            ctx,
		Namespace:          "payments",
		ServiceAccountName: "checkout-sa",
		ServiceAccountUID:  "uid-sa-2",
	})
	if err != nil {
		t.Fatalf("NewKubernetesServiceAccountTokenPostureEnvelope(recreated) error = %v", err)
	}
	if first.StableFactKey == recreated.StableFactKey {
		t.Fatalf("token posture stable key did not change across ServiceAccount UID recreation: %q", first.StableFactKey)
	}
}

func TestNewKubernetesWorkloadIdentityAndIRSAEnvelopes(t *testing.T) {
	ctx := testKubernetesContext()
	usage, err := NewKubernetesWorkloadIdentityUseEnvelope(KubernetesWorkloadIdentityUseObservation{
		Context:                      ctx,
		WorkloadObjectID:             "workload:hashed-id",
		WorkloadKind:                 "deployment",
		Namespace:                    "payments",
		ServiceAccountName:           "checkout-sa",
		ServiceAccountUID:            "uid-sa-1",
		ProjectedServiceAccountToken: true,
	})
	if err != nil {
		t.Fatalf("NewKubernetesWorkloadIdentityUseEnvelope() error = %v", err)
	}
	assertFact(t, usage, facts.KubernetesWorkloadIdentityUseFactKind)
	if payloadValueContains(usage.Payload, "payments") || payloadValueContains(usage.Payload, "checkout-sa") {
		t.Fatalf("workload identity payload leaked raw namespace/service account: %#v", usage.Payload)
	}

	irsa, err := NewEKSIRSAAnnotationEnvelope(EKSIRSAAnnotationObservation{
		Context:            ctx,
		Namespace:          "payments",
		ServiceAccountName: "checkout-sa",
		ServiceAccountUID:  "uid-sa-1",
		RoleARN:            "arn:aws:iam::123456789012:role/checkout",
		AnnotationPresent:  true,
	})
	if err != nil {
		t.Fatalf("NewEKSIRSAAnnotationEnvelope() error = %v", err)
	}
	assertFact(t, irsa, facts.EKSIRSAAnnotationFactKind)
	assertPayloadString(t, irsa.Payload, "role_arn", "arn:aws:iam::123456789012:role/checkout")
	if payloadValueContains(irsa.Payload, "payments") || payloadValueContains(irsa.Payload, "checkout-sa") {
		t.Fatalf("IRSA payload leaked raw namespace/service account: %#v", irsa.Payload)
	}
}

func TestNewKubernetesGCPWorkloadIdentityBindingEnvelopeRedactsAnnotationTarget(t *testing.T) {
	ctx := testKubernetesContext()
	targetEmail := "app@demo-proj.iam.gserviceaccount.com"
	workloadPool := "demo-proj.svc.id.goog"
	env, err := NewKubernetesGCPWorkloadIdentityBindingEnvelope(
		KubernetesGCPWorkloadIdentityBindingObservation{
			Context:                ctx,
			Namespace:              "payments",
			ServiceAccountName:     "checkout-sa",
			ServiceAccountUID:      "uid-sa-1",
			GCPServiceAccountEmail: targetEmail,
			GCPWorkloadPool:        workloadPool,
			AnnotationPresent:      true,
		},
	)
	if err != nil {
		t.Fatalf("NewKubernetesGCPWorkloadIdentityBindingEnvelope() error = %v", err)
	}
	assertFact(t, env, facts.KubernetesGCPWorkloadIdentityBindingFactKind)
	if got := env.Payload["gcp_service_account_email_digest"]; got != GCPServiceAccountEmailDigest(targetEmail) {
		t.Fatalf("gcp_service_account_email_digest = %v", got)
	}
	if got := env.Payload["gcp_workload_identity_subject_fingerprint"]; got != GCPWorkloadIdentitySubjectFingerprint(workloadPool, "payments", "checkout-sa") {
		t.Fatalf("gcp_workload_identity_subject_fingerprint = %v", got)
	}
	if env.Payload["service_account_join_key"] == "" {
		t.Fatal("service_account_join_key is empty")
	}
	forbidden := []string{targetEmail, workloadPool, "payments", "checkout-sa"}
	for _, value := range forbidden {
		if payloadValueContains(env.Payload, value) {
			t.Fatalf("GCP Workload Identity payload leaked raw identity %q: %#v", value, env.Payload)
		}
	}
}

func TestNewKubernetesGCPWorkloadIdentityBindingEnvelopeRequiresPoolAndAnnotation(t *testing.T) {
	ctx := testKubernetesContext()
	base := KubernetesGCPWorkloadIdentityBindingObservation{
		Context:                ctx,
		Namespace:              "payments",
		ServiceAccountName:     "checkout-sa",
		GCPServiceAccountEmail: "app@demo-proj.iam.gserviceaccount.com",
		GCPWorkloadPool:        "demo-proj.svc.id.goog",
		AnnotationPresent:      true,
	}
	missingPool := base
	missingPool.GCPWorkloadPool = ""
	if _, err := NewKubernetesGCPWorkloadIdentityBindingEnvelope(missingPool); err == nil {
		t.Fatal("expected error when GCP workload pool is empty")
	}
	missingAnnotation := base
	missingAnnotation.GCPServiceAccountEmail = ""
	if _, err := NewKubernetesGCPWorkloadIdentityBindingEnvelope(missingAnnotation); err == nil {
		t.Fatal("expected error when GCP service-account annotation is empty")
	}
}

func TestNewEKSPodIdentityAssociationEnvelopeRedactsServiceAccount(t *testing.T) {
	ctx := testKubernetesContext()
	env, err := NewEKSPodIdentityAssociationEnvelope(EKSPodIdentityAssociationObservation{
		Context:            ctx,
		AssociationID:      "pia-123",
		ClusterName:        "prod-eks",
		Namespace:          "payments",
		ServiceAccountName: "checkout-sa",
		RoleARN:            "arn:aws:iam::123456789012:role/checkout",
	})
	if err != nil {
		t.Fatalf("NewEKSPodIdentityAssociationEnvelope() error = %v", err)
	}
	assertFact(t, env, facts.EKSPodIdentityAssociationFactKind)
	assertPayloadString(t, env.Payload, "role_arn", "arn:aws:iam::123456789012:role/checkout")
	if payloadValueContains(env.Payload, "payments") || payloadValueContains(env.Payload, "checkout-sa") {
		t.Fatalf("Pod Identity payload leaked raw namespace/service account: %#v", env.Payload)
	}
}

func testKubernetesContext() KubernetesContext {
	return KubernetesContext{
		ClusterID:           "prod-us-east-1",
		ScopeID:             "kubernetes_live:cluster-1",
		GenerationID:        "kubernetes_live:gen-1",
		CollectorInstanceID: "k8s-prod",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 6, 2, 13, 0, 0, 0, time.UTC),
	}
}

func payloadValueContains(payload map[string]any, needle string) bool {
	for _, value := range payload {
		if anyValueContains(value, needle) {
			return true
		}
	}
	return false
}

func anyValueContains(value any, needle string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, needle)
	case []string:
		for _, item := range typed {
			if strings.Contains(item, needle) {
				return true
			}
		}
	case map[string]string:
		for key, item := range typed {
			if strings.Contains(key, needle) || strings.Contains(item, needle) {
				return true
			}
		}
	case map[string]any:
		return payloadValueContains(typed, needle)
	case []map[string]any:
		for _, item := range typed {
			if payloadValueContains(item, needle) {
				return true
			}
		}
	}
	return false
}
