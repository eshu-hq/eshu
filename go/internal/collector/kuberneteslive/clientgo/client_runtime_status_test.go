// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package clientgo

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestAdapterMapsDeploymentReplicaStatus proves that ListDeployments reads
// .Spec.Replicas (DESIRED) and .Status.ReadyReplicas/.Status.AvailableReplicas
// (OBSERVED) onto the workload's runtime-status fields, and leaves PodPhase nil
// (a Deployment carries no pod phase) (#5431).
func TestAdapterMapsDeploymentReplicaStatus(t *testing.T) {
	t.Parallel()

	desired := int32(3)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "checkout", UID: "uid-d"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &desired,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "web", Image: "docker.io/myapp/web:v1.2.3"}},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas:     2,
			AvailableReplicas: 1,
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
	if workload.DesiredReplicas == nil || *workload.DesiredReplicas != 3 {
		t.Fatalf("DesiredReplicas = %v, want 3", workload.DesiredReplicas)
	}
	if workload.ReadyReplicas == nil || *workload.ReadyReplicas != 2 {
		t.Fatalf("ReadyReplicas = %v, want 2", workload.ReadyReplicas)
	}
	if workload.AvailableReplicas == nil || *workload.AvailableReplicas != 1 {
		t.Fatalf("AvailableReplicas = %v, want 1", workload.AvailableReplicas)
	}
	if workload.PodPhase != nil {
		t.Fatalf("PodPhase = %v, want nil (Deployments carry no pod phase)", *workload.PodPhase)
	}
}

// TestAdapterDeploymentNilSpecReplicasLeavesDesiredNil proves that a
// Deployment with an unset .Spec.Replicas leaves DesiredReplicas nil rather
// than fabricating a zero value (#5431).
func TestAdapterDeploymentNilSpecReplicasLeavesDesiredNil(t *testing.T) {
	t.Parallel()

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "checkout", UID: "uid-d"},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "web", Image: "docker.io/myapp/web:v1.2.3"}},
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
	if got := result.Items[0].DesiredReplicas; got != nil {
		t.Fatalf("DesiredReplicas = %v, want nil (Spec.Replicas unset)", *got)
	}
}

// TestAdapterMapsReplicaSetReplicaStatus proves that ListReplicaSets reads
// .Spec.Replicas (DESIRED) and .Status.ReadyReplicas/.Status.AvailableReplicas
// (OBSERVED) onto the workload's runtime-status fields, and leaves PodPhase nil
// (a ReplicaSet carries no pod phase) (#5431).
func TestAdapterMapsReplicaSetReplicaStatus(t *testing.T) {
	t.Parallel()

	desired := int32(5)
	replicaset := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "checkout-7f8d9", UID: "uid-rs"},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: &desired,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "web", Image: "docker.io/myapp/web:v1.2.3"}},
				},
			},
		},
		Status: appsv1.ReplicaSetStatus{
			ReadyReplicas:     4,
			AvailableReplicas: 4,
		},
	}
	adapter := NewAdapter(fake.NewClientset(replicaset))

	result, err := adapter.ListReplicaSets(context.Background())
	if err != nil {
		t.Fatalf("ListReplicaSets() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("replicaset count = %d, want 1", len(result.Items))
	}
	workload := result.Items[0]
	if workload.DesiredReplicas == nil || *workload.DesiredReplicas != 5 {
		t.Fatalf("DesiredReplicas = %v, want 5", workload.DesiredReplicas)
	}
	if workload.ReadyReplicas == nil || *workload.ReadyReplicas != 4 {
		t.Fatalf("ReadyReplicas = %v, want 4", workload.ReadyReplicas)
	}
	if workload.AvailableReplicas == nil || *workload.AvailableReplicas != 4 {
		t.Fatalf("AvailableReplicas = %v, want 4", workload.AvailableReplicas)
	}
	if workload.PodPhase != nil {
		t.Fatalf("PodPhase = %v, want nil (ReplicaSets carry no pod phase)", *workload.PodPhase)
	}
}

// TestAdapterMapsPodPhase proves that ListPods reads .Status.Phase onto the
// workload's PodPhase field, and leaves the replica fields nil (a Pod carries
// no replica spec or status) (#5431).
func TestAdapterMapsPodPhase(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "checkout-abc123", UID: "uid-pod"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "web", Image: "docker.io/myapp/web:v1.2.3"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
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
	if workload.PodPhase == nil || *workload.PodPhase != "Running" {
		t.Fatalf("PodPhase = %v, want \"Running\"", workload.PodPhase)
	}
	if workload.DesiredReplicas != nil {
		t.Fatalf("DesiredReplicas = %v, want nil (Pods carry no replica spec)", *workload.DesiredReplicas)
	}
	if workload.ReadyReplicas != nil {
		t.Fatalf("ReadyReplicas = %v, want nil (Pods carry no replica status)", *workload.ReadyReplicas)
	}
	if workload.AvailableReplicas != nil {
		t.Fatalf("AvailableReplicas = %v, want nil (Pods carry no replica status)", *workload.AvailableReplicas)
	}
}

// TestAdapterPodEmptyPhaseLeavesPodPhaseNil proves that a Pod observed with an
// empty .Status.Phase (not yet scheduled) leaves PodPhase nil rather than
// emitting an empty string (#5431).
func TestAdapterPodEmptyPhaseLeavesPodPhaseNil(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "checkout-abc123", UID: "uid-pod"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "web", Image: "docker.io/myapp/web:v1.2.3"}},
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
	if got := result.Items[0].PodPhase; got != nil {
		t.Fatalf("PodPhase = %v, want nil (empty phase not observed)", *got)
	}
}
