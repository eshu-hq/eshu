// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package clientgo_test exercises the real Adapter output through the
// kubernetes_live.pod_template wire payload for the #5433 workload kinds,
// proving the package AGENTS.md METADATA-ONLY invariant: a rich
// StatefulSet/DaemonSet .Status must never leak beyond the mapped
// desired_replicas/ready_replicas/available_replicas values.
package clientgo_test

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/eshu-hq/eshu/go/internal/collector/kuberneteslive"
	"github.com/eshu-hq/eshu/go/internal/collector/kuberneteslive/clientgo"
)

// Sentinel strings planted across non-allowed .Status fields in the fixtures
// below. None of these must ever reach the emitted pod_template payload.
const (
	sentinelStatefulSetCondReason  = "SENTINEL-STS-COND-REASON-c7a3d1e2"
	sentinelStatefulSetCondMessage = "SENTINEL-STS-COND-MSG-c7a3d1e2"
	sentinelStatefulSetRevision    = "SENTINEL-STS-REVISION-c7a3d1e2"
)

// TestPodTemplateRedactionExcludesStatefulSetStatusDetail proves that a
// StatefulSet with a rich .Status (conditions, currentReplicas,
// updatedReplicas, currentRevision, updateRevision, observedGeneration,
// collisionCount) drives the real adapter (ListStatefulSets) and envelope
// emission end to end, and that the emitted kubernetes_live.pod_template
// payload carries ONLY desired_replicas/ready_replicas/available_replicas from
// that status -- none of the other .Status data (#5433).
func TestPodTemplateRedactionExcludesStatefulSetStatusDetail(t *testing.T) {
	t.Parallel()

	desired := int32(3)
	collisionCount := int32(2)
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "checkout-db", UID: "uid-ss"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &desired,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout-db"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "db", Image: "ghcr.io/acme/checkout-db:1"}},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ObservedGeneration: 11,
			Replicas:           3,
			ReadyReplicas:      2,
			CurrentReplicas:    3,
			UpdatedReplicas:    1,
			AvailableReplicas:  1,
			CurrentRevision:    sentinelStatefulSetRevision,
			UpdateRevision:     sentinelStatefulSetRevision,
			CollisionCount:     &collisionCount,
			Conditions: []appsv1.StatefulSetCondition{{
				Type:    "RollingUpdate",
				Status:  corev1.ConditionTrue,
				Reason:  sentinelStatefulSetCondReason,
				Message: sentinelStatefulSetCondMessage,
			}},
		},
	}

	payload := mustPodTemplatePayload(t, statefulSetWorkload(t, statefulset))

	if got := payload["desired_replicas"]; got != int32(3) {
		t.Fatalf("desired_replicas = %#v, want 3", got)
	}
	if got := payload["ready_replicas"]; got != int32(2) {
		t.Fatalf("ready_replicas = %#v, want 2", got)
	}
	if got := payload["available_replicas"]; got != int32(1) {
		t.Fatalf("available_replicas = %#v, want 1", got)
	}
	if _, present := payload["pod_phase"]; present {
		t.Fatalf("pod_phase present = %#v, want absent for a StatefulSet", payload["pod_phase"])
	}
	assertPayloadOmitsKeys(t, payload,
		"replicas", "current_replicas", "updated_replicas",
		"current_revision", "update_revision",
		"observed_generation", "collision_count", "conditions")
	assertPayloadOmitsSentinels(t, payload,
		sentinelStatefulSetCondReason, sentinelStatefulSetCondMessage, sentinelStatefulSetRevision)
}

// Sentinel strings planted across non-allowed DaemonSet .Status fields.
const (
	sentinelDaemonSetCondReason  = "SENTINEL-DS-COND-REASON-c7a3d1e2"
	sentinelDaemonSetCondMessage = "SENTINEL-DS-COND-MSG-c7a3d1e2"
)

// TestPodTemplateRedactionExcludesDaemonSetStatusDetail proves that a
// DaemonSet with a rich .Status (misscheduled/updated/unavailable counts,
// observedGeneration, conditions) drives the real adapter (ListDaemonSets)
// and envelope emission end to end, and that the emitted
// kubernetes_live.pod_template payload carries ONLY the three mapped
// replica-equivalent fields from that status -- none of the other .Status
// data (#5433).
func TestPodTemplateRedactionExcludesDaemonSetStatusDetail(t *testing.T) {
	t.Parallel()

	daemonset := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "node-agent", UID: "uid-ds"},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "node-agent"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "agent", Image: "ghcr.io/acme/node-agent:1"}},
				},
			},
		},
		Status: appsv1.DaemonSetStatus{
			CurrentNumberScheduled: 6,
			NumberMisscheduled:     1,
			DesiredNumberScheduled: 6,
			NumberReady:            5,
			ObservedGeneration:     9,
			UpdatedNumberScheduled: 4,
			NumberAvailable:        4,
			NumberUnavailable:      2,
			Conditions: []appsv1.DaemonSetCondition{{
				Type:    "SpeedUpdate",
				Status:  corev1.ConditionFalse,
				Reason:  sentinelDaemonSetCondReason,
				Message: sentinelDaemonSetCondMessage,
			}},
		},
	}

	payload := mustPodTemplatePayload(t, daemonSetWorkload(t, daemonset))

	if got := payload["desired_replicas"]; got != int32(6) {
		t.Fatalf("desired_replicas = %#v, want 6 (DesiredNumberScheduled)", got)
	}
	if got := payload["ready_replicas"]; got != int32(5) {
		t.Fatalf("ready_replicas = %#v, want 5 (NumberReady)", got)
	}
	if got := payload["available_replicas"]; got != int32(4) {
		t.Fatalf("available_replicas = %#v, want 4 (NumberAvailable)", got)
	}
	if _, present := payload["pod_phase"]; present {
		t.Fatalf("pod_phase present = %#v, want absent for a DaemonSet", payload["pod_phase"])
	}
	assertPayloadOmitsKeys(t, payload,
		"current_number_scheduled", "number_misscheduled", "updated_number_scheduled",
		"number_unavailable", "observed_generation", "conditions")
	assertPayloadOmitsSentinels(t, payload, sentinelDaemonSetCondReason, sentinelDaemonSetCondMessage)
}

func statefulSetWorkload(t *testing.T, statefulset *appsv1.StatefulSet) kuberneteslive.WorkloadObject {
	t.Helper()
	adapter := clientgo.NewAdapter(fake.NewClientset(statefulset))
	result, err := adapter.ListStatefulSets(context.Background())
	if err != nil {
		t.Fatalf("ListStatefulSets() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("statefulset count = %d, want 1", len(result.Items))
	}
	return result.Items[0]
}

func daemonSetWorkload(t *testing.T, daemonset *appsv1.DaemonSet) kuberneteslive.WorkloadObject {
	t.Helper()
	adapter := clientgo.NewAdapter(fake.NewClientset(daemonset))
	result, err := adapter.ListDaemonSets(context.Background())
	if err != nil {
		t.Fatalf("ListDaemonSets() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("daemonset count = %d, want 1", len(result.Items))
	}
	return result.Items[0]
}
