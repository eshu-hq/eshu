// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package clientgo_test also exercises the real Adapter output through the
// kubernetes_live.namespace wire payload, proving the package AGENTS.md
// METADATA-ONLY invariant for the new #5434 fact kind: a rich Namespace
// object (annotations, status, finalizers, managed fields) must never leak
// beyond the namespace's labels.
package clientgo_test

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/eshu-hq/eshu/go/internal/collector/kuberneteslive"
	"github.com/eshu-hq/eshu/go/internal/collector/kuberneteslive/clientgo"
)

// Sentinel strings planted across non-label namespace fields. None of these
// must ever reach the emitted kubernetes_live.namespace payload.
const (
	sentinelNamespaceAnnotationKey    = "sentinel.example.com/annotation"
	sentinelNamespaceAnnotationValue  = "SENTINEL-NS-ANNOTATION-VALUE-c19e2a4d"
	sentinelNamespaceStatusReason     = "SENTINEL-NS-STATUS-REASON-c19e2a4d"
	sentinelNamespaceStatusMessage    = "SENTINEL-NS-STATUS-MESSAGE-c19e2a4d"
	sentinelNamespaceFinalizer        = "sentinel.example.com/SENTINEL-NS-FINALIZER-c19e2a4d"
	sentinelNamespaceManagedFieldsMgr = "SENTINEL-NS-MANAGER-c19e2a4d"
)

// TestNamespaceRedactionExcludesNamespaceObjectDetail proves that a
// Namespace with a rich metadata/status shape (annotations, status
// phase/conditions, finalizers, managed fields) drives the real adapter
// (ListNamespaces) and envelope emission (NewNamespaceEnvelope) end to end,
// and that the emitted kubernetes_live.namespace payload carries ONLY the
// namespace's labels -- no other namespace object detail (issue #5434).
func TestNamespaceRedactionExcludesNamespaceObjectDetail(t *testing.T) {
	t.Parallel()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "payments-prod",
			UID:  "uid-ns",
			Labels: map[string]string{
				"environment":               "prod",
				"app.kubernetes.io/name":    "payments",
				"app.kubernetes.io/part-of": "checkout",
			},
			Annotations: map[string]string{
				sentinelNamespaceAnnotationKey: sentinelNamespaceAnnotationValue,
			},
			Finalizers: []string{sentinelNamespaceFinalizer},
			ManagedFields: []metav1.ManagedFieldsEntry{
				{Manager: sentinelNamespaceManagedFieldsMgr},
			},
		},
		Spec: corev1.NamespaceSpec{
			Finalizers: []corev1.FinalizerName{corev1.FinalizerKubernetes},
		},
		Status: corev1.NamespaceStatus{
			Phase: corev1.NamespaceActive,
			Conditions: []corev1.NamespaceCondition{{
				Type:    corev1.NamespaceDeletionContentFailure,
				Status:  corev1.ConditionTrue,
				Reason:  sentinelNamespaceStatusReason,
				Message: sentinelNamespaceStatusMessage,
			}},
		},
	}

	payload := mustNamespacePayload(t, namespaceObjectMeta(t, namespace))

	wantLabels := map[string]any{
		"app.kubernetes.io/name":    "payments",
		"app.kubernetes.io/part-of": "checkout",
		"environment":               "prod",
	}
	gotLabels, ok := payload["labels"].(map[string]string)
	if !ok {
		t.Fatalf("payload[labels] = %#v, want map[string]string", payload["labels"])
	}
	if len(gotLabels) != len(wantLabels) {
		t.Fatalf("labels = %#v, want %#v", gotLabels, wantLabels)
	}
	for key, want := range wantLabels {
		if got := gotLabels[key]; got != want {
			t.Fatalf("labels[%q] = %q, want %q", key, got, want)
		}
	}

	if got := payload["namespace"]; got != "payments-prod" {
		t.Fatalf("namespace = %#v, want %q", got, "payments-prod")
	}
	if got := payload["cluster_id"]; got != "prod-us-east-1" {
		t.Fatalf("cluster_id = %#v, want %q", got, "prod-us-east-1")
	}

	assertNamespacePayloadOmitsKeys(t, payload,
		"annotations", "status", "phase", "conditions", "finalizers",
		"managed_fields", "spec", "uid", "resource_version")
	assertNamespacePayloadOmitsSentinels(t, payload,
		sentinelNamespaceAnnotationKey, sentinelNamespaceAnnotationValue,
		sentinelNamespaceStatusReason, sentinelNamespaceStatusMessage,
		sentinelNamespaceFinalizer, sentinelNamespaceManagedFieldsMgr)
}

// TestNamespaceRedactionOmitsAnnotationsKeyWhenNil proves that a namespace
// with no labels at all still emits a valid payload with no "annotations"
// key present -- the field is reserved for #5444 and this collector never
// populates it (scope: labels only for #5434).
func TestNamespaceRedactionOmitsAnnotationsKeyWhenNil(t *testing.T) {
	t.Parallel()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "default", UID: "uid-default"},
	}
	payload := mustNamespacePayload(t, namespaceObjectMeta(t, namespace))
	if _, present := payload["annotations"]; present {
		t.Fatalf("payload[annotations] present = %#v, want absent (reserved for #5444)", payload["annotations"])
	}
	if _, present := payload["labels"]; present {
		t.Fatalf("payload[labels] present = %#v, want absent for an unlabeled namespace", payload["labels"])
	}
}

func namespaceObjectMeta(t *testing.T, namespace *corev1.Namespace) kuberneteslive.ObjectMeta {
	t.Helper()
	adapter := clientgo.NewAdapter(fake.NewClientset(namespace))
	result, err := adapter.ListNamespaces(context.Background())
	if err != nil {
		t.Fatalf("ListNamespaces() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("namespace count = %d, want 1", len(result.Items))
	}
	return result.Items[0]
}

// mustNamespacePayload wires an adapter-produced ObjectMeta into a
// NamespaceObservation exactly as generationBuilder.collectNamespaces does
// (plain field copies, no branching logic to fake), then drives it through
// the real NewNamespaceEnvelope encode seam and returns the wire payload.
func mustNamespacePayload(t *testing.T, meta kuberneteslive.ObjectMeta) map[string]any {
	t.Helper()
	identity := kuberneteslive.ObjectIdentity{
		ClusterID: "prod-us-east-1",
		APIGroup:  meta.APIGroup,
		Version:   meta.Version,
		Resource:  meta.Resource,
		Namespace: meta.Namespace,
		Name:      meta.Name,
		UID:       meta.UID,
	}
	envelope, err := kuberneteslive.NewNamespaceEnvelope(kuberneteslive.NamespaceObservation{
		Identity:            identity,
		Labels:              meta.Labels,
		GenerationID:        "gen-1",
		CollectorInstanceID: "k8s-prod",
	})
	if err != nil {
		t.Fatalf("NewNamespaceEnvelope() error = %v", err)
	}
	return envelope.Payload
}

func assertNamespacePayloadOmitsKeys(t *testing.T, payload map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if value, present := payload[key]; present {
			t.Fatalf("payload contains disallowed key %q = %#v, want absent", key, value)
		}
	}
}

func assertNamespacePayloadOmitsSentinels(t *testing.T, payload map[string]any, sentinels ...string) {
	t.Helper()
	for _, sentinel := range sentinels {
		if namespacePayloadContainsSubstring(payload, sentinel) {
			t.Fatalf("payload leaked non-allowed namespace object data containing %q: %#v", sentinel, payload)
		}
	}
}

func namespacePayloadContainsSubstring(payload map[string]any, needle string) bool {
	for _, value := range payload {
		if namespaceValueContainsSubstring(value, needle) {
			return true
		}
	}
	return false
}

func namespaceValueContainsSubstring(value any, needle string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, needle)
	case []string:
		for _, item := range typed {
			if strings.Contains(item, needle) {
				return true
			}
		}
	case map[string]any:
		return namespacePayloadContainsSubstring(typed, needle)
	case map[string]string:
		for k, v := range typed {
			if strings.Contains(k, needle) || strings.Contains(v, needle) {
				return true
			}
		}
	}
	return false
}
