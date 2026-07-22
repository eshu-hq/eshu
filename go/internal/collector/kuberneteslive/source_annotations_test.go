// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kuberneteslive

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSourceEmitsWorkloadAnnotations locks the end-to-end #5471 F2 wiring: an
// annotation observed on a live workload's ObjectMeta (the client-go adapter
// boundary) must survive generationBuilder.addWorkload into the emitted
// kubernetes_live.pod_template envelope's payload, carrying the ArgoCD
// argocd.argoproj.io/tracking-id declared->live identity signal Lane B's
// BINDS_LIVE_WORKLOAD edge will consume.
func TestSourceEmitsWorkloadAnnotations(t *testing.T) {
	t.Parallel()

	deployment := WorkloadObject{
		Meta: ObjectMeta{
			APIGroup: "apps", Version: "v1", Resource: "deployments",
			Namespace: "payments", Name: "checkout", UID: "uid-deploy",
			Annotations: map[string]string{
				"argocd.argoproj.io/tracking-id": "checkout:apps/Deployment:payments/checkout",
			},
		},
		ServiceAccount: "checkout-sa",
		Containers:     []ContainerSummary{{Name: "app", Image: "img:1"}},
	}
	client := &fakeClient{
		namespaces:  ListResult[ObjectMeta]{Items: []ObjectMeta{{Version: "v1", Resource: "namespaces", Name: "payments", UID: "uid-ns"}}},
		deployments: ListResult[WorkloadObject]{Items: []WorkloadObject{deployment}},
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
	podTemplates := envelopesOfKind(envs, facts.KubernetesPodTemplateFactKind)
	if len(podTemplates) != 1 {
		t.Fatalf("pod_template facts = %d, want 1", len(podTemplates))
	}
	annotations, ok := podTemplates[0].Payload["annotations"].(map[string]string)
	if !ok {
		t.Fatalf("payload annotations = %#v, want map[string]string", podTemplates[0].Payload["annotations"])
	}
	if got := annotations["argocd.argoproj.io/tracking-id"]; got != "checkout:apps/Deployment:payments/checkout" {
		t.Fatalf("payload annotations tracking-id = %q, want %q", got, "checkout:apps/Deployment:payments/checkout")
	}
}
