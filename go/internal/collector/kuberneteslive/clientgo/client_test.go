// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package clientgo

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/eshu-hq/eshu/go/internal/collector/kuberneteslive"
)

func TestAdapterMapsDeploymentMetadataOnly(t *testing.T) {
	t.Parallel()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "payments", Name: "checkout", UID: "uid-d",
			Annotations: map[string]string{"argocd.argoproj.io/tracking-id": "checkout:apps/Deployment:payments/checkout"},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: "checkout-sa",
					Containers: []corev1.Container{{
						Name:  "app",
						Image: "ghcr.io/acme/checkout:1",
						Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
						Env: []corev1.EnvVar{
							{Name: "PLAIN", Value: "plain-value-should-not-leak"},
							{Name: "FROM_SECRET", ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{Key: "k"},
							}},
						},
					}},
				},
			},
		},
	}
	adapter := NewAdapter(fake.NewClientset(deployment))

	result, err := adapter.ListDeployments(context.Background())
	if err != nil {
		t.Fatalf("ListDeployments() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("deployment count = %d, want 1", len(result.Items))
	}
	workload := result.Items[0]
	if workload.Meta.Resource != "deployments" || workload.Meta.APIGroup != "apps" {
		t.Fatalf("meta GVR mismatch: %+v", workload.Meta)
	}
	if got := workload.Meta.Annotations["argocd.argoproj.io/tracking-id"]; got != "checkout:apps/Deployment:payments/checkout" {
		t.Fatalf("meta annotations tracking-id = %q, want %q", got, "checkout:apps/Deployment:payments/checkout")
	}
	if workload.ServiceAccount != "checkout-sa" {
		t.Fatalf("service account = %q", workload.ServiceAccount)
	}
	if len(workload.Containers) != 1 {
		t.Fatalf("container count = %d, want 1", len(workload.Containers))
	}
	container := workload.Containers[0]
	// Env var NAMES are kept; the plaintext value must never appear.
	wantKeys := map[string]bool{"PLAIN": true, "FROM_SECRET": true}
	for _, key := range container.EnvKeys {
		if !wantKeys[key] {
			t.Fatalf("unexpected env key %q", key)
		}
		delete(wantKeys, key)
	}
	if len(wantKeys) != 0 {
		t.Fatalf("missing env keys: %v", wantKeys)
	}
	if !container.EnvFromSecret {
		t.Fatalf("EnvFromSecret = false, want true (secret key ref present)")
	}
	// The plaintext env value must not be carried anywhere on the summary.
	if container.Image == "plain-value-should-not-leak" {
		t.Fatalf("image field leaked an env value")
	}
}

func TestAdapterMapsServiceAccountsMetadataOnly(t *testing.T) {
	t.Parallel()

	automount := false
	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "payments",
			Name:            "checkout-sa",
			UID:             "uid-sa",
			ResourceVersion: "rv-123",
			Annotations: map[string]string{
				"eks.amazonaws.com/role-arn":     "arn:aws:iam::123456789012:role/checkout",
				"iam.gke.io/gcp-service-account": "app@demo-proj.iam.gserviceaccount.com",
				"vault.hashicorp.com/token-leak": "vault-token-value-should-not-leak",
			},
		},
		AutomountServiceAccountToken: &automount,
		Secrets:                      []corev1.ObjectReference{{Name: "checkout-token-secret"}},
		ImagePullSecrets:             []corev1.LocalObjectReference{{Name: "private-registry-secret"}},
	}
	adapter := NewAdapter(fake.NewClientset(serviceAccount))

	result, err := adapter.ListServiceAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListServiceAccounts() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("service account count = %d, want 1", len(result.Items))
	}
	got := result.Items[0]
	if got.Meta.Resource != "serviceaccounts" || got.Meta.Namespace != "payments" || got.Meta.Name != "checkout-sa" {
		t.Fatalf("service account metadata mismatch: %+v", got.Meta)
	}
	if got.AutomountToken == nil || *got.AutomountToken {
		t.Fatalf("AutomountToken = %v, want false", got.AutomountToken)
	}
	if got.SecretRefCount != 1 || got.ImagePullSecretRefCount != 1 {
		t.Fatalf("secret ref counts = %d/%d, want 1/1", got.SecretRefCount, got.ImagePullSecretRefCount)
	}
	if got.IRSAAnnotation != "arn:aws:iam::123456789012:role/checkout" {
		t.Fatalf("IRSAAnnotation = %q", got.IRSAAnnotation)
	}
	if got.GCPServiceAccountAnnotation != "app@demo-proj.iam.gserviceaccount.com" {
		t.Fatalf("GCPServiceAccountAnnotation = %q", got.GCPServiceAccountAnnotation)
	}
	for _, value := range got.AnnotationKeys {
		if value == "vault-token-value-should-not-leak" ||
			value == "checkout-token-secret" ||
			value == "private-registry-secret" {
			t.Fatalf("metadata-only service account mapping leaked sensitive value/name %q", value)
		}
	}
}

func TestAdapterMapsRBACMetadataOnly(t *testing.T) {
	t.Parallel()

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "secret-reader", UID: "uid-role"},
		Rules: []rbacv1.PolicyRule{{
			Verbs:         []string{"get", "list"},
			APIGroups:     []string{""},
			Resources:     []string{"secrets"},
			ResourceNames: []string{"checkout-token-secret"},
		}},
	}
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-reader", UID: "uid-cr"},
		Rules:      []rbacv1.PolicyRule{{Verbs: []string{"get"}, Resources: []string{"pods"}}},
	}
	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "read-secrets", UID: "uid-rb"},
		RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "secret-reader"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: "payments", Name: "checkout-sa"}},
	}
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-reader-binding", UID: "uid-crb"},
		RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "cluster-reader"},
		Subjects:   []rbacv1.Subject{{Kind: "Group", Name: "platform-admins"}},
	}
	adapter := NewAdapter(fake.NewClientset(role, clusterRole, roleBinding, clusterRoleBinding))

	roles, err := adapter.ListRoles(context.Background())
	if err != nil {
		t.Fatalf("ListRoles() error = %v", err)
	}
	if len(roles.Items) != 1 || roles.Items[0].Kind != kuberneteslive.RBACRoleKindRole {
		t.Fatalf("roles = %+v, want one Role", roles.Items)
	}
	if !roles.Items[0].Rules[0].ResourceNamesPresent || roles.Items[0].Rules[0].ResourceNameCount != 1 {
		t.Fatalf("resource-name summary = %+v, want presence/count only", roles.Items[0].Rules[0])
	}

	clusterRoles, err := adapter.ListClusterRoles(context.Background())
	if err != nil {
		t.Fatalf("ListClusterRoles() error = %v", err)
	}
	if len(clusterRoles.Items) != 1 || clusterRoles.Items[0].Kind != kuberneteslive.RBACRoleKindClusterRole {
		t.Fatalf("clusterRoles = %+v, want one ClusterRole", clusterRoles.Items)
	}

	bindings, err := adapter.ListRoleBindings(context.Background())
	if err != nil {
		t.Fatalf("ListRoleBindings() error = %v", err)
	}
	if len(bindings.Items) != 1 || bindings.Items[0].Kind != kuberneteslive.BindingKindRoleBinding {
		t.Fatalf("bindings = %+v, want one RoleBinding", bindings.Items)
	}
	if bindings.Items[0].Subjects[0].Name != "checkout-sa" {
		t.Fatalf("subject mapping lost service account join identity: %+v", bindings.Items[0].Subjects[0])
	}

	clusterBindings, err := adapter.ListClusterRoleBindings(context.Background())
	if err != nil {
		t.Fatalf("ListClusterRoleBindings() error = %v", err)
	}
	if len(clusterBindings.Items) != 1 || clusterBindings.Items[0].Kind != kuberneteslive.BindingKindClusterRoleBinding {
		t.Fatalf("clusterBindings = %+v, want one ClusterRoleBinding", clusterBindings.Items)
	}
}

func TestAdapterMapsProjectedServiceAccountTokenPosture(t *testing.T) {
	t.Parallel()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "checkout", UID: "uid-d"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: "checkout-sa",
					Volumes: []corev1.Volume{{
						Name: "token",
						VolumeSource: corev1.VolumeSource{
							Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{{
								ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Audience: "api"},
							}}},
						},
					}},
				},
			},
		},
	}
	adapter := NewAdapter(fake.NewClientset(deployment))
	result, err := adapter.ListDeployments(context.Background())
	if err != nil {
		t.Fatalf("ListDeployments() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("deployment count = %d, want 1", len(result.Items))
	}
	if !result.Items[0].ProjectedServiceAccountToken {
		t.Fatalf("ProjectedServiceAccountToken = false, want true")
	}
}

func TestAdapterMapsServicesAndIngress(t *testing.T) {
	t.Parallel()

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: "n", Name: "web-svc", UID: "uid-s"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "web"}},
	}
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Namespace: "n", Name: "web-ing", UID: "uid-i"},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{{
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{Name: "web-svc"},
							},
						}},
					},
				},
			}},
		},
	}
	adapter := NewAdapter(fake.NewClientset(service, ingress))

	svcResult, err := adapter.ListServices(context.Background())
	if err != nil {
		t.Fatalf("ListServices() error = %v", err)
	}
	if len(svcResult.Items) != 1 || svcResult.Items[0].Selector["app"] != "web" {
		t.Fatalf("service mapping wrong: %+v", svcResult.Items)
	}

	ingResult, err := adapter.ListIngresses(context.Background())
	if err != nil {
		t.Fatalf("ListIngresses() error = %v", err)
	}
	if len(ingResult.Items) != 1 {
		t.Fatalf("ingress count = %d, want 1", len(ingResult.Items))
	}
	backends := ingResult.Items[0].BackendServices
	if len(backends) != 1 || backends[0] != "web-svc" {
		t.Fatalf("ingress backends = %v, want [web-svc]", backends)
	}
}

func TestAdapterForbiddenListIsPartial(t *testing.T) {
	t.Parallel()

	client := fake.NewClientset()
	client.PrependReactor("list", "services", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Group: "", Resource: "services"}, "", errors.New("rbac: forbidden"),
		)
	})
	adapter := NewAdapter(client)

	result, err := adapter.ListServices(context.Background())
	if err != nil {
		t.Fatalf("ListServices() should not hard-fail on forbidden; err = %v", err)
	}
	if !result.Partial {
		t.Fatalf("forbidden list should be partial")
	}
	if result.Reason != kuberneteslive.WarningForbiddenResource {
		t.Fatalf("partial reason = %q, want forbidden_resource", result.Reason)
	}
}

func TestAdapterImplementsClientInterface(t *testing.T) {
	t.Parallel()

	var _ kuberneteslive.Client = NewAdapter(fake.NewClientset())
}

func TestAuthConfigRequiresKubeconfigPath(t *testing.T) {
	t.Parallel()

	if _, err := (AuthConfig{Mode: AuthModeKubeconfig}).RESTConfig(); err == nil {
		t.Fatalf("expected error for missing kubeconfig path")
	}
	if _, err := (AuthConfig{Mode: AuthMode("bogus")}).RESTConfig(); err == nil {
		t.Fatalf("expected error for unsupported auth mode")
	}
}

// TestAdapterMapsPodContainerStatusDigest proves that ListPods reads
// pod.Status.ContainerStatuses[].ImageID and normalizes it via
// kuberneteslive.NormalizeCRIImageID to set ResolvedImageDigest on the
// corresponding container. A Deployment object carries no status, so its
// containers have no resolved digests (#5432).
func TestAdapterMapsPodContainerStatusDigest(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: "web-abc123", UID: "uid-pod"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "web",
				Image: "docker.io/myapp/web:v1.2.3",
			}},
			InitContainers: []corev1.Container{{
				Name:  "init-setup",
				Image: "docker.io/myapp/init:latest",
			}},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:    "web",
				ImageID: "docker-pullable://docker.io/myapp/web@sha256:aaaabbbbccccddddeeeeffff1111222233334444555566667777888899990000",
			}},
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name:    "init-setup",
				ImageID: "docker://docker.io/myapp/init@sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff",
			}},
		},
	}
	adapter := NewAdapter(fake.NewClientset(pod))

	result, err := adapter.ListPods(context.Background())
	if err != nil {
		t.Fatalf("ListPods() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("pod count = %d, want 1", len(result.Items))
	}
	workload := result.Items[0]
	if len(workload.Containers) != 2 {
		t.Fatalf("container count = %d, want 2 (1 init + 1 regular)", len(workload.Containers))
	}

	// Init container should have a resolved digest.
	initContainer := workload.Containers[0]
	if !initContainer.Init {
		t.Fatalf("first container should be init container")
	}
	if got, want := initContainer.ResolvedImageDigest, "docker.io/myapp/init@sha256:1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"; got != want {
		t.Fatalf("init container ResolvedImageDigest = %q, want %q", got, want)
	}

	// Regular container should have a resolved digest from docker-pullable://.
	webContainer := workload.Containers[1]
	if webContainer.Init {
		t.Fatalf("second container should be regular (not init)")
	}
	if got, want := webContainer.ResolvedImageDigest, "docker.io/myapp/web@sha256:aaaabbbbccccddddeeeeffff1111222233334444555566667777888899990000"; got != want {
		t.Fatalf("web container ResolvedImageDigest = %q, want %q", got, want)
	}
}

// TestAdapterDeploymentHasNoResolvedDigest proves that a Deployment (which
// carries only the pod template spec, no status) maps containers with no
// resolved digests — byte-identical to today (#5432 non-regression).
func TestAdapterDeploymentHasNoResolvedDigest(t *testing.T) {
	t.Parallel()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "app", Name: "web", UID: "uid-d"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "web",
						Image: "docker.io/myapp/web:v1.2.3",
					}},
				},
			},
		},
	}
	adapter := NewAdapter(fake.NewClientset(deployment))

	result, err := adapter.ListDeployments(context.Background())
	if err != nil {
		t.Fatalf("ListDeployments() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("deployment count = %d, want 1", len(result.Items))
	}
	container := result.Items[0].Containers[0]
	if container.ResolvedImageDigest != "" {
		t.Fatalf("Deployment container ResolvedImageDigest = %q, want empty (Deployments carry no status)", container.ResolvedImageDigest)
	}
}
