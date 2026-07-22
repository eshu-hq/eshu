// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	"reflect"
	"testing"

	kuberneteslivev1 "github.com/eshu-hq/eshu/sdk/go/factschema/kuberneteslive/v1"
)

// TestDecodeKubernetesLivePodTemplate_AnnotationsRoundTrip proves the optional
// Annotations map on kuberneteslivev1.PodTemplate — the #5471 F2 foundation
// that carries the ArgoCD tracking-id declared->live identity signal — round
// trips through EncodeKubernetesLivePodTemplate -> DecodeKubernetesLivePodTemplate
// unchanged, including the argocd.argoproj.io/tracking-id key.
func TestDecodeKubernetesLivePodTemplate_AnnotationsRoundTrip(t *testing.T) {
	t.Parallel()

	annotations := map[string]string{
		"argocd.argoproj.io/tracking-id": "checkout:apps/Deployment:payments/checkout",
		"kubectl.kubernetes.io/other":    "value",
	}
	original := kuberneteslivev1.PodTemplate{
		ObjectID:    "kube-obj:demo-cluster:apps-v1-deployments:payments:checkout:1a2b3c4d",
		Annotations: annotations,
	}

	payload, err := EncodeKubernetesLivePodTemplate(original)
	if err != nil {
		t.Fatalf("EncodeKubernetesLivePodTemplate() error = %v, want nil", err)
	}
	got, ok := payload["annotations"].(map[string]string)
	if !ok {
		t.Fatalf("payload annotations = %#v, want map[string]string", payload["annotations"])
	}
	if got["argocd.argoproj.io/tracking-id"] != "checkout:apps/Deployment:payments/checkout" {
		t.Fatalf("payload annotations tracking-id = %q, want %q", got["argocd.argoproj.io/tracking-id"], "checkout:apps/Deployment:payments/checkout")
	}

	decoded, err := DecodeKubernetesLivePodTemplate(testEnvelope(payload))
	if err != nil {
		t.Fatalf("DecodeKubernetesLivePodTemplate() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("DecodeKubernetesLivePodTemplate() = %+v, want %+v", decoded, original)
	}
}

// TestDecodeKubernetesLivePodTemplate_AnnotationsAbsentStaysNil proves backward
// compatibility: a fact emitted before the annotations field existed (or by a
// collector that observed no annotations) omits the "annotations" key
// entirely, and the decoded struct's Annotations field stays nil rather than
// an empty map — the existing PodTemplate decode contract for every other
// optional map field (Selector, Labels).
func TestDecodeKubernetesLivePodTemplate_AnnotationsAbsentStaysNil(t *testing.T) {
	t.Parallel()

	original := kuberneteslivev1.PodTemplate{
		ObjectID: "kube-obj:demo-cluster:apps-v1-deployments:payments:checkout:1a2b3c4d",
	}

	payload, err := EncodeKubernetesLivePodTemplate(original)
	if err != nil {
		t.Fatalf("EncodeKubernetesLivePodTemplate() error = %v, want nil", err)
	}
	if _, present := payload["annotations"]; present {
		t.Fatalf("payload annotations present = %#v, want key omitted when not observed", payload["annotations"])
	}

	decoded, err := DecodeKubernetesLivePodTemplate(testEnvelope(payload))
	if err != nil {
		t.Fatalf("DecodeKubernetesLivePodTemplate() error = %v, want nil", err)
	}
	if decoded.Annotations != nil {
		t.Fatalf("Annotations = %v, want nil", decoded.Annotations)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("DecodeKubernetesLivePodTemplate() = %+v, want %+v", decoded, original)
	}
}
