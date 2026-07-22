// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package clientgo_test exercises the real Adapter output through the
// kubernetes_live.pod_template wire payload, proving the package AGENTS.md
// METADATA-ONLY invariant for the #5431 runtime-status fields: a rich
// Deployment/ReplicaSet/Pod .Status must never leak beyond the four allowed
// values (desired_replicas, ready_replicas, available_replicas, pod_phase).
package clientgo_test

import (
	"context"
	"strings"
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
	sentinelDeployCondReason     = "SENTINEL-DEPLOY-COND-REASON-b6f2b6f9"
	sentinelDeployCondMessage    = "SENTINEL-DEPLOY-COND-MSG-b6f2b6f9"
	sentinelRSCondReason         = "SENTINEL-RS-COND-REASON-b6f2b6f9"
	sentinelRSCondMessage        = "SENTINEL-RS-COND-MSG-b6f2b6f9"
	sentinelPodMessage           = "SENTINEL-POD-MESSAGE-b6f2b6f9"
	sentinelPodReason            = "SENTINEL-POD-REASON-b6f2b6f9"
	sentinelPodCondReason        = "SENTINEL-POD-COND-REASON-b6f2b6f9"
	sentinelPodCondMessage       = "SENTINEL-POD-COND-MSG-b6f2b6f9"
	sentinelNominatedNode        = "SENTINEL-NOMINATED-NODE-b6f2b6f9"
	sentinelHostIP               = "10.99.99.99"
	sentinelPodIP                = "10.88.88.88"
	sentinelContainerWaitReason  = "SENTINEL-CONTAINER-WAIT-REASON-b6f2b6f9"
	sentinelContainerWaitMessage = "SENTINEL-CONTAINER-WAIT-MSG-b6f2b6f9"
)

// TestPodTemplateRedactionExcludesDeploymentStatusDetail proves that a
// Deployment with a rich .Status (conditions, updatedReplicas,
// unavailableReplicas, observedGeneration, collisionCount) drives the real
// adapter (ListDeployments) and envelope emission (NewPodTemplateEnvelope)
// end to end, and that the emitted kubernetes_live.pod_template payload
// carries ONLY desired_replicas/ready_replicas/available_replicas from that
// status -- none of the other .Status data (#5431).
func TestPodTemplateRedactionExcludesDeploymentStatusDetail(t *testing.T) {
	t.Parallel()

	desired := int32(3)
	collisionCount := int32(9)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "checkout", UID: "uid-d"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &desired,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "ghcr.io/acme/checkout:1"}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration:  42,
			Replicas:            3,
			UpdatedReplicas:     3,
			ReadyReplicas:       2,
			AvailableReplicas:   1,
			UnavailableReplicas: 1,
			CollisionCount:      &collisionCount,
			Conditions: []appsv1.DeploymentCondition{{
				Type:    appsv1.DeploymentAvailable,
				Status:  corev1.ConditionFalse,
				Reason:  sentinelDeployCondReason,
				Message: sentinelDeployCondMessage,
			}},
		},
	}

	payload := mustPodTemplatePayload(t, deploymentWorkload(t, deployment))

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
		t.Fatalf("pod_phase present = %#v, want absent for a Deployment", payload["pod_phase"])
	}
	assertPayloadOmitsKeys(t, payload,
		"replicas", "updated_replicas", "unavailable_replicas",
		"observed_generation", "collision_count", "conditions")
	assertPayloadOmitsSentinels(t, payload, sentinelDeployCondReason, sentinelDeployCondMessage)
}

// TestPodTemplateRedactionExcludesReplicaSetStatusDetail proves the same
// invariant for a ReplicaSet with a rich .Status (conditions,
// fullyLabeledReplicas, observedGeneration) (#5431).
func TestPodTemplateRedactionExcludesReplicaSetStatusDetail(t *testing.T) {
	t.Parallel()

	desired := int32(5)
	replicaset := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "checkout-7f8d9", UID: "uid-rs"},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: &desired,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "ghcr.io/acme/checkout:1"}},
				},
			},
		},
		Status: appsv1.ReplicaSetStatus{
			Replicas:             5,
			FullyLabeledReplicas: 5,
			ReadyReplicas:        4,
			AvailableReplicas:    4,
			ObservedGeneration:   7,
			Conditions: []appsv1.ReplicaSetCondition{{
				Type:    appsv1.ReplicaSetReplicaFailure,
				Status:  corev1.ConditionTrue,
				Reason:  sentinelRSCondReason,
				Message: sentinelRSCondMessage,
			}},
		},
	}

	payload := mustPodTemplatePayload(t, replicaSetWorkload(t, replicaset))

	if got := payload["desired_replicas"]; got != int32(5) {
		t.Fatalf("desired_replicas = %#v, want 5", got)
	}
	if got := payload["ready_replicas"]; got != int32(4) {
		t.Fatalf("ready_replicas = %#v, want 4", got)
	}
	if got := payload["available_replicas"]; got != int32(4) {
		t.Fatalf("available_replicas = %#v, want 4", got)
	}
	if _, present := payload["pod_phase"]; present {
		t.Fatalf("pod_phase present = %#v, want absent for a ReplicaSet", payload["pod_phase"])
	}
	assertPayloadOmitsKeys(t, payload,
		"replicas", "fully_labeled_replicas",
		"observed_generation", "conditions")
	assertPayloadOmitsSentinels(t, payload, sentinelRSCondReason, sentinelRSCondMessage)
}

// TestPodTemplateRedactionExcludesPodStatusDetail proves the same invariant
// for a Pod with a rich .Status (conditions, host/pod IP, nominated node,
// pod-level message/reason, and a container status with a waiting
// reason/message) (#5431).
func TestPodTemplateRedactionExcludesPodStatusDetail(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "checkout-abc123", UID: "uid-pod"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "app", Image: "ghcr.io/acme/checkout:1"}},
		},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			Message:           sentinelPodMessage,
			Reason:            sentinelPodReason,
			NominatedNodeName: sentinelNominatedNode,
			HostIP:            sentinelHostIP,
			PodIP:             sentinelPodIP,
			QOSClass:          corev1.PodQOSBurstable,
			Conditions: []corev1.PodCondition{{
				Type:    corev1.PodReady,
				Status:  corev1.ConditionTrue,
				Reason:  sentinelPodCondReason,
				Message: sentinelPodCondMessage,
			}},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:         "app",
				Ready:        true,
				RestartCount: 5,
				Image:        "ghcr.io/acme/checkout:1",
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{
						Reason:  sentinelContainerWaitReason,
						Message: sentinelContainerWaitMessage,
					},
				},
			}},
		},
	}

	payload := mustPodTemplatePayload(t, podWorkload(t, pod))

	if got := payload["pod_phase"]; got != "Running" {
		t.Fatalf("pod_phase = %#v, want %q", got, "Running")
	}
	for _, key := range []string{"desired_replicas", "ready_replicas", "available_replicas"} {
		if _, present := payload[key]; present {
			t.Fatalf("%s present = %#v, want absent for a Pod", key, payload[key])
		}
	}
	assertPayloadOmitsKeys(t, payload,
		"message", "reason", "nominated_node_name", "host_ip", "pod_ip",
		"qos_class", "conditions", "container_statuses")
	assertPayloadOmitsSentinels(t, payload,
		sentinelPodMessage, sentinelPodReason, sentinelNominatedNode,
		sentinelHostIP, sentinelPodIP, sentinelPodCondReason, sentinelPodCondMessage,
		sentinelContainerWaitReason, sentinelContainerWaitMessage)
}

func deploymentWorkload(t *testing.T, deployment *appsv1.Deployment) kuberneteslive.WorkloadObject {
	t.Helper()
	adapter := clientgo.NewAdapter(fake.NewClientset(deployment))
	result, err := adapter.ListDeployments(context.Background())
	if err != nil {
		t.Fatalf("ListDeployments() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("deployment count = %d, want 1", len(result.Items))
	}
	return result.Items[0]
}

func replicaSetWorkload(t *testing.T, replicaset *appsv1.ReplicaSet) kuberneteslive.WorkloadObject {
	t.Helper()
	adapter := clientgo.NewAdapter(fake.NewClientset(replicaset))
	result, err := adapter.ListReplicaSets(context.Background())
	if err != nil {
		t.Fatalf("ListReplicaSets() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("replicaset count = %d, want 1", len(result.Items))
	}
	return result.Items[0]
}

func podWorkload(t *testing.T, pod *corev1.Pod) kuberneteslive.WorkloadObject {
	t.Helper()
	adapter := clientgo.NewAdapter(fake.NewClientset(pod))
	result, err := adapter.ListPods(context.Background())
	if err != nil {
		t.Fatalf("ListPods() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("pod count = %d, want 1", len(result.Items))
	}
	return result.Items[0]
}

// mustPodTemplatePayload wires an adapter-produced WorkloadObject into a
// PodTemplateObservation exactly as generationBuilder.addWorkload does (plain
// field copies, no branching logic to fake), then drives it through the real
// NewPodTemplateEnvelope encode seam and returns the wire payload.
func mustPodTemplatePayload(t *testing.T, workload kuberneteslive.WorkloadObject) map[string]any {
	t.Helper()
	identity := kuberneteslive.ObjectIdentity{
		ClusterID: "prod-us-east-1",
		APIGroup:  workload.Meta.APIGroup,
		Version:   workload.Meta.Version,
		Resource:  workload.Meta.Resource,
		Namespace: workload.Meta.Namespace,
		Name:      workload.Meta.Name,
		UID:       workload.Meta.UID,
	}
	envelope, err := kuberneteslive.NewPodTemplateEnvelope(kuberneteslive.PodTemplateObservation{
		Identity:            identity,
		Containers:          workload.Containers,
		ServiceAccount:      workload.ServiceAccount,
		Selector:            workload.Selector,
		Labels:              workload.Meta.Labels,
		Annotations:         workload.Meta.Annotations,
		GenerationID:        "gen-1",
		CollectorInstanceID: "k8s-prod",
		DesiredReplicas:     workload.DesiredReplicas,
		ReadyReplicas:       workload.ReadyReplicas,
		AvailableReplicas:   workload.AvailableReplicas,
		PodPhase:            workload.PodPhase,
	})
	if err != nil {
		t.Fatalf("NewPodTemplateEnvelope() error = %v", err)
	}
	return envelope.Payload
}

func assertPayloadOmitsKeys(t *testing.T, payload map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if value, present := payload[key]; present {
			t.Fatalf("payload contains disallowed key %q = %#v, want absent", key, value)
		}
	}
}

func assertPayloadOmitsSentinels(t *testing.T, payload map[string]any, sentinels ...string) {
	t.Helper()
	for _, sentinel := range sentinels {
		if payloadContainsSubstring(payload, sentinel) {
			t.Fatalf("payload leaked non-allowed .Status data containing %q: %#v", sentinel, payload)
		}
	}
}

func payloadContainsSubstring(payload map[string]any, needle string) bool {
	for _, value := range payload {
		if valueContainsSubstring(value, needle) {
			return true
		}
	}
	return false
}

func valueContainsSubstring(value any, needle string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, needle)
	case []string:
		for _, item := range typed {
			if strings.Contains(item, needle) {
				return true
			}
		}
	case []map[string]any:
		for _, item := range typed {
			if payloadContainsSubstring(item, needle) {
				return true
			}
		}
	case map[string]any:
		return payloadContainsSubstring(typed, needle)
	case map[string]string:
		for k, v := range typed {
			if strings.Contains(k, needle) || strings.Contains(v, needle) {
				return true
			}
		}
	}
	return false
}
