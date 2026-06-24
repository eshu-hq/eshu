// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kuberneteslive

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSourceEmitsSecretsIAMKubernetesRBACFacts(t *testing.T) {
	t.Parallel()

	automount := false
	serviceAccount := ServiceAccountObject{
		Meta: ObjectMeta{
			Version: "v1", Resource: "serviceaccounts",
			Namespace: "payments", Name: "checkout-sa", UID: "uid-sa",
		},
		AnnotationKeys: []string{"eks.amazonaws.com/role-arn"},
		IRSAAnnotation: "arn:aws:iam::123456789012:role/checkout",
		AutomountToken: &automount,
		SecretRefCount: 2,
	}
	role := RBACRoleObject{
		Meta: ObjectMeta{
			APIGroup: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles",
			Namespace: "payments", Name: "secret-reader", UID: "uid-role",
		},
		Kind: RBACRoleKindRole,
		Rules: []RBACRuleSummary{{
			Verbs:                []string{"get", "list"},
			APIGroups:            []string{""},
			Resources:            []string{"secrets"},
			ResourceNameCount:    1,
			ResourceNamesPresent: true,
		}},
	}
	clusterRole := RBACRoleObject{
		Meta: ObjectMeta{
			APIGroup: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles",
			Name: "cluster-reader", UID: "uid-clusterrole",
		},
		Kind:  RBACRoleKindClusterRole,
		Rules: []RBACRuleSummary{{Verbs: []string{"get"}, Resources: []string{"pods"}}},
	}
	roleBinding := RBACBindingObject{
		Meta: ObjectMeta{
			APIGroup: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings",
			Namespace: "payments", Name: "read-secrets", UID: "uid-rb",
		},
		Kind:        BindingKindRoleBinding,
		RoleRefKind: RBACRoleKindRole,
		RoleRefName: "secret-reader",
		Subjects: []RBACSubject{{
			Kind: "ServiceAccount", Namespace: "payments", Name: "checkout-sa",
		}},
	}
	clusterRoleBinding := RBACBindingObject{
		Meta: ObjectMeta{
			APIGroup: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings",
			Name: "cluster-reader-binding", UID: "uid-crb",
		},
		Kind:        BindingKindClusterRoleBinding,
		RoleRefKind: RBACRoleKindClusterRole,
		RoleRefName: "cluster-reader",
		Subjects: []RBACSubject{{
			Kind: "Group", Name: "platform-admins",
		}},
	}
	deployment := WorkloadObject{
		Meta: ObjectMeta{
			APIGroup: "apps", Version: "v1", Resource: "deployments",
			Namespace: "payments", Name: "checkout", UID: "uid-deploy",
		},
		ServiceAccount:               "checkout-sa",
		ProjectedServiceAccountToken: true,
	}
	client := &fakeClient{
		serviceAccounts:     ListResult[ServiceAccountObject]{Items: []ServiceAccountObject{serviceAccount}},
		roles:               ListResult[RBACRoleObject]{Items: []RBACRoleObject{role}},
		clusterRoles:        ListResult[RBACRoleObject]{Items: []RBACRoleObject{clusterRole}},
		roleBindings:        ListResult[RBACBindingObject]{Items: []RBACBindingObject{roleBinding}},
		clusterRoleBindings: ListResult[RBACBindingObject]{Items: []RBACBindingObject{clusterRoleBinding}},
		deployments:         ListResult[WorkloadObject]{Items: []WorkloadObject{deployment}},
	}

	source := newSource(client)
	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatalf("Next() ok = false, want true")
	}
	envs := drain(t, collected.Facts)
	if got := countKind(envs, facts.KubernetesServiceAccountFactKind); got != 1 {
		t.Fatalf("service account facts = %d, want 1", got)
	}
	if got := countKind(envs, facts.KubernetesServiceAccountTokenPostureFactKind); got != 1 {
		t.Fatalf("token posture facts = %d, want 1", got)
	}
	if got := countKind(envs, facts.EKSIRSAAnnotationFactKind); got != 1 {
		t.Fatalf("IRSA annotation facts = %d, want 1", got)
	}
	if got := countKind(envs, facts.KubernetesRBACRoleFactKind); got != 2 {
		t.Fatalf("RBAC role facts = %d, want 2", got)
	}
	bindings := envelopesOfKind(envs, facts.KubernetesRBACBindingFactKind)
	if len(bindings) != 2 {
		t.Fatalf("RBAC binding facts = %d, want 2", len(bindings))
	}
	seenScopes := map[string]bool{}
	for _, binding := range bindings {
		scopeValue, _ := binding.Payload["binding_scope"].(string)
		seenScopes[scopeValue] = true
		if payloadContains(binding.Payload, "payments") ||
			payloadContains(binding.Payload, "checkout-sa") ||
			payloadContains(binding.Payload, "platform-admins") {
			t.Fatalf("binding payload leaked raw subject names: %#v", binding.Payload)
		}
	}
	if !seenScopes[BindingScopeNamespace] || !seenScopes[BindingScopeCluster] {
		t.Fatalf("binding scopes = %v, want namespace and cluster", seenScopes)
	}
	if got := countKind(envs, facts.KubernetesWorkloadIdentityUseFactKind); got != 1 {
		t.Fatalf("workload identity use facts = %d, want 1", got)
	}
}

func TestSourceEmitsGCPWorkloadIdentityBindingOnlyWithConfiguredPool(t *testing.T) {
	t.Parallel()

	serviceAccount := ServiceAccountObject{
		Meta: ObjectMeta{
			Version: "v1", Resource: "serviceaccounts",
			Namespace: "payments", Name: "checkout-sa", UID: "uid-sa",
		},
		AnnotationKeys:              []string{"iam.gke.io/gcp-service-account"},
		GCPServiceAccountAnnotation: "app@demo-proj.iam.gserviceaccount.com",
	}
	client := &fakeClient{
		serviceAccounts: ListResult[ServiceAccountObject]{Items: []ServiceAccountObject{serviceAccount}},
	}

	withPool := &Source{
		Config: Config{
			CollectorInstanceID: "k8s-prod",
			Clusters: []ClusterTarget{{
				ClusterID:       "prod-gke",
				FencingToken:    3,
				GCPWorkloadPool: "demo-proj.svc.id.goog",
			}},
		},
		ClientFactory: factoryFor(client),
		Clock:         fixedClock(),
	}
	collected, ok, err := withPool.Next(context.Background())
	if err != nil {
		t.Fatalf("Next(withPool) error = %v", err)
	}
	if !ok {
		t.Fatalf("Next(withPool) ok = false, want true")
	}
	envs := drain(t, collected.Facts)
	bindings := envelopesOfKind(envs, facts.KubernetesGCPWorkloadIdentityBindingFactKind)
	if len(bindings) != 1 {
		t.Fatalf("GCP Workload Identity binding facts = %d, want 1: %#v", len(bindings), envs)
	}
	binding := bindings[0]
	for _, forbidden := range []string{
		"app@demo-proj.iam.gserviceaccount.com",
		"demo-proj.svc.id.goog",
		"payments",
		"checkout-sa",
	} {
		if payloadContains(binding.Payload, forbidden) {
			t.Fatalf("binding payload leaked raw identity %q: %#v", forbidden, binding.Payload)
		}
	}

	withoutPool := newSource(client)
	collected, ok, err = withoutPool.Next(context.Background())
	if err != nil {
		t.Fatalf("Next(withoutPool) error = %v", err)
	}
	if !ok {
		t.Fatalf("Next(withoutPool) ok = false, want true")
	}
	envs = drain(t, collected.Facts)
	if got := countKind(envs, facts.KubernetesGCPWorkloadIdentityBindingFactKind); got != 0 {
		t.Fatalf("GCP Workload Identity binding facts without pool = %d, want 0", got)
	}
}

func TestSourceRBACForbiddenListEmitsCoverageWarning(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		roles: ListResult[RBACRoleObject]{Partial: true, Reason: WarningForbiddenResource},
	}
	source := newSource(client)
	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatalf("Next() ok = false, want true")
	}
	if collected.Generation.FreshnessHint != "partial" {
		t.Fatalf("FreshnessHint = %q, want partial", collected.Generation.FreshnessHint)
	}
	envs := drain(t, collected.Facts)
	if got := countKind(envs, facts.SecretsIAMCoverageWarningFactKind); got != 1 {
		t.Fatalf("secrets/IAM coverage warnings = %d, want 1", got)
	}
}
