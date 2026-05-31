package clientgo

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
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
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "checkout", UID: "uid-d"},
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
