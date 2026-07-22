// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package clientgo_test proves the real Adapter's annotation capture stays
// allowlisted to the declared->live identity-binding keys #5471 F2 needs,
// never the full Kubernetes ObjectMeta.Annotations map. Annotation values are
// unbounded (kubectl.kubernetes.io/last-applied-configuration commonly runs
// KB-tens of KB and can embed secret material), so copying the full map into
// every collected workload's pod_template fact would be an unbounded,
// potentially secret-leaking hot-path cost.
package clientgo_test

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestPodTemplateAnnotationsAllowlistFiltersUnboundedMap drives a Deployment
// carrying a mix of allowlisted identity-binding annotations and
// bloat/secret-shaped annotations through the real Adapter (ListDeployments)
// and the real NewPodTemplateEnvelope encode seam. Only the allowlisted
// identity keys must survive into the emitted kubernetes_live.pod_template
// payload's annotations field; everything else, including a
// kubectl.kubernetes.io/last-applied-configuration value shaped like the
// KB-scale secret-bearing blob Kubernetes actually produces, must be dropped.
func TestPodTemplateAnnotationsAllowlistFiltersUnboundedMap(t *testing.T) {
	t.Parallel()

	// A last-applied-configuration annotation is commonly the full applied
	// manifest re-serialized as JSON, which can run tens of KB and can embed
	// Secret data (e.g. a Secret applied via kubectl apply carries its own
	// last-applied-configuration with the Secret's stringData/data inline).
	// Simulate that shape with a large synthetic blob standing in for
	// arbitrary embedded content.
	lastApplied := `{"apiVersion":"apps/v1","kind":"Deployment","secretLookingField":"` +
		strings.Repeat("SENTINEL-BLOAT-", 512) + `"}`

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "payments",
			Name:      "checkout",
			UID:       "uid-d",
			Annotations: map[string]string{
				"argocd.argoproj.io/tracking-id":                   "checkout:apps/Deployment:payments/checkout",
				"app.kubernetes.io/instance":                       "checkout",
				"app.kubernetes.io/name":                           "checkout-service",
				"kubectl.kubernetes.io/last-applied-configuration": lastApplied,
				"example.com/whatever":                             "arbitrary-non-identity-annotation",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "ghcr.io/acme/checkout:1"}},
				},
			},
		},
	}

	payload := mustPodTemplatePayload(t, deploymentWorkload(t, deployment))

	annotations, ok := payload["annotations"].(map[string]string)
	if !ok {
		t.Fatalf("payload annotations = %#v, want map[string]string", payload["annotations"])
	}

	want := map[string]string{
		"argocd.argoproj.io/tracking-id": "checkout:apps/Deployment:payments/checkout",
		"app.kubernetes.io/instance":     "checkout",
		"app.kubernetes.io/name":         "checkout-service",
	}
	if len(annotations) != len(want) {
		t.Fatalf("payload annotations = %#v, want exactly %#v", annotations, want)
	}
	for key, value := range want {
		if got := annotations[key]; got != value {
			t.Fatalf("payload annotations[%q] = %q, want %q", key, got, value)
		}
	}

	if _, present := annotations["kubectl.kubernetes.io/last-applied-configuration"]; present {
		t.Fatalf("payload annotations leaked kubectl.kubernetes.io/last-applied-configuration, want dropped: %#v", annotations)
	}
	if _, present := annotations["example.com/whatever"]; present {
		t.Fatalf("payload annotations leaked non-identity key example.com/whatever, want dropped: %#v", annotations)
	}
	for key, value := range annotations {
		if strings.Contains(value, "SENTINEL-BLOAT-") {
			t.Fatalf("payload annotations[%q] leaked the last-applied-configuration blob, want it dropped entirely: %#v", key, annotations)
		}
	}
}

// TestPodTemplateAnnotationsAbsentWhenNoAllowlistedKeys proves backward
// compatibility: a workload whose only annotations are non-identity keys
// (bloat, secret-shaped, or otherwise) emits a nil/absent annotations field,
// exactly as if the workload carried no annotations at all.
func TestPodTemplateAnnotationsAbsentWhenNoAllowlistedKeys(t *testing.T) {
	t.Parallel()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "payments",
			Name:      "checkout",
			UID:       "uid-d",
			Annotations: map[string]string{
				"kubectl.kubernetes.io/last-applied-configuration": `{"apiVersion":"apps/v1"}`,
				"example.com/whatever":                             "arbitrary-non-identity-annotation",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "ghcr.io/acme/checkout:1"}},
				},
			},
		},
	}

	payload := mustPodTemplatePayload(t, deploymentWorkload(t, deployment))

	if value, present := payload["annotations"]; present {
		t.Fatalf("payload annotations = %#v, want absent when no allowlisted keys are present", value)
	}
}
