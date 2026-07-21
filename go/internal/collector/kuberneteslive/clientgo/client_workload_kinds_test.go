// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package clientgo

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestAdapterMapsStatefulSetReplicaStatus proves that ListStatefulSets reads
// .Spec.Replicas (DESIRED) and .Status.ReadyReplicas/.Status.AvailableReplicas
// (OBSERVED) onto the workload's runtime-status fields, and leaves PodPhase
// nil (a StatefulSet carries no pod phase), mirroring the
// Deployment/ReplicaSet mapping (#5433).
func TestAdapterMapsStatefulSetReplicaStatus(t *testing.T) {
	t.Parallel()

	desired := int32(3)
	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "checkout-db", UID: "uid-ss"},
		Spec: appsv1.StatefulSetSpec{
			Replicas: &desired,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout-db"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "db", Image: "docker.io/myapp/db:v1.2.3"}},
				},
			},
		},
		Status: appsv1.StatefulSetStatus{
			ReadyReplicas:     2,
			AvailableReplicas: 1,
		},
	}
	adapter := NewAdapter(fake.NewClientset(statefulset))

	result, err := adapter.ListStatefulSets(context.Background())
	if err != nil {
		t.Fatalf("ListStatefulSets() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("statefulset count = %d, want 1", len(result.Items))
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
		t.Fatalf("PodPhase = %v, want nil (StatefulSets carry no pod phase)", *workload.PodPhase)
	}
}

// TestAdapterStatefulSetNilSpecReplicasLeavesDesiredNil proves that a
// StatefulSet with an unset .Spec.Replicas leaves DesiredReplicas nil rather
// than fabricating a zero value, mirroring the Deployment/ReplicaSet path
// (#5433).
func TestAdapterStatefulSetNilSpecReplicasLeavesDesiredNil(t *testing.T) {
	t.Parallel()

	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: "payments", Name: "checkout-db", UID: "uid-ss"},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "checkout-db"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "db", Image: "docker.io/myapp/db:v1.2.3"}},
				},
			},
		},
	}
	adapter := NewAdapter(fake.NewClientset(statefulset))

	result, err := adapter.ListStatefulSets(context.Background())
	if err != nil {
		t.Fatalf("ListStatefulSets() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("statefulset count = %d, want 1", len(result.Items))
	}
	if got := result.Items[0].DesiredReplicas; got != nil {
		t.Fatalf("DesiredReplicas = %v, want nil (Spec.Replicas unset)", *got)
	}
}

// TestAdapterMapsDaemonSetSchedulingCounts proves that ListDaemonSets maps
// .Status.DesiredNumberScheduled/.Status.NumberReady/.Status.NumberAvailable
// onto the workload's replica-equivalent fields (a DaemonSet has no replica
// spec), and leaves PodPhase nil (#5433).
func TestAdapterMapsDaemonSetSchedulingCounts(t *testing.T) {
	t.Parallel()

	daemonset := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "node-agent", UID: "uid-ds"},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "node-agent"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "agent", Image: "docker.io/myapp/agent:v1"}},
				},
			},
		},
		Status: appsv1.DaemonSetStatus{
			DesiredNumberScheduled: 6,
			NumberReady:            5,
			NumberAvailable:        4,
		},
	}
	adapter := NewAdapter(fake.NewClientset(daemonset))

	result, err := adapter.ListDaemonSets(context.Background())
	if err != nil {
		t.Fatalf("ListDaemonSets() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("daemonset count = %d, want 1", len(result.Items))
	}
	workload := result.Items[0]
	if workload.DesiredReplicas == nil || *workload.DesiredReplicas != 6 {
		t.Fatalf("DesiredReplicas = %v, want 6 (DesiredNumberScheduled)", workload.DesiredReplicas)
	}
	if workload.ReadyReplicas == nil || *workload.ReadyReplicas != 5 {
		t.Fatalf("ReadyReplicas = %v, want 5 (NumberReady)", workload.ReadyReplicas)
	}
	if workload.AvailableReplicas == nil || *workload.AvailableReplicas != 4 {
		t.Fatalf("AvailableReplicas = %v, want 4 (NumberAvailable)", workload.AvailableReplicas)
	}
	if workload.PodPhase != nil {
		t.Fatalf("PodPhase = %v, want nil (DaemonSets carry no pod phase)", *workload.PodPhase)
	}
}

// TestAdapterDaemonSetZeroSchedulingCountsAreObserved proves that a DaemonSet
// observed with all-zero scheduling counts (no nodes matched yet) reports
// zero, not nil -- unlike DesiredReplicas on Deployment/ReplicaSet/StatefulSet,
// DaemonSetStatus fields are not optional pointers, so an observed zero is a
// real observation, not an absent value (#5433).
func TestAdapterDaemonSetZeroSchedulingCountsAreObserved(t *testing.T) {
	t.Parallel()

	daemonset := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "node-agent", UID: "uid-ds"},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "node-agent"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "agent", Image: "docker.io/myapp/agent:v1"}},
				},
			},
		},
	}
	adapter := NewAdapter(fake.NewClientset(daemonset))

	result, err := adapter.ListDaemonSets(context.Background())
	if err != nil {
		t.Fatalf("ListDaemonSets() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("daemonset count = %d, want 1", len(result.Items))
	}
	workload := result.Items[0]
	if workload.DesiredReplicas == nil || *workload.DesiredReplicas != 0 {
		t.Fatalf("DesiredReplicas = %v, want 0 (observed, not absent)", workload.DesiredReplicas)
	}
	if workload.ReadyReplicas == nil || *workload.ReadyReplicas != 0 {
		t.Fatalf("ReadyReplicas = %v, want 0 (observed, not absent)", workload.ReadyReplicas)
	}
	if workload.AvailableReplicas == nil || *workload.AvailableReplicas != 0 {
		t.Fatalf("AvailableReplicas = %v, want 0 (observed, not absent)", workload.AvailableReplicas)
	}
}

// TestAdapterMapsJobPodSpecOnlyNoReplicaStatus proves that ListJobs emits the
// pod template spec via workloadFromPodSpec and leaves every runtime-status
// field nil -- a Job has no replica concept (#5433).
func TestAdapterMapsJobPodSpecOnlyNoReplicaStatus(t *testing.T) {
	t.Parallel()

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Namespace: "batch", Name: "nightly-export", UID: "uid-job"},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "export", Image: "docker.io/myapp/export:v1"}},
				},
			},
		},
		Status: batchv1.JobStatus{
			Active:    1,
			Succeeded: 2,
			Failed:    1,
		},
	}
	adapter := NewAdapter(fake.NewClientset(job))

	result, err := adapter.ListJobs(context.Background())
	if err != nil {
		t.Fatalf("ListJobs() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("job count = %d, want 1", len(result.Items))
	}
	workload := result.Items[0]
	if len(workload.Containers) != 1 || workload.Containers[0].Image != "docker.io/myapp/export:v1" {
		t.Fatalf("Containers = %+v, want one export container", workload.Containers)
	}
	if workload.DesiredReplicas != nil {
		t.Fatalf("DesiredReplicas = %v, want nil (Jobs have no replica concept)", *workload.DesiredReplicas)
	}
	if workload.ReadyReplicas != nil {
		t.Fatalf("ReadyReplicas = %v, want nil (Jobs have no replica concept)", *workload.ReadyReplicas)
	}
	if workload.AvailableReplicas != nil {
		t.Fatalf("AvailableReplicas = %v, want nil (Jobs have no replica concept)", *workload.AvailableReplicas)
	}
	if workload.PodPhase != nil {
		t.Fatalf("PodPhase = %v, want nil (Jobs carry no pod phase)", *workload.PodPhase)
	}
}

// TestAdapterMapsCronJobNestedPodSpecOnlyNoReplicaStatus proves that
// ListCronJobs extracts the pod template spec from the nested
// .Spec.JobTemplate.Spec.Template.Spec location and leaves every
// runtime-status field nil -- a CronJob has no replica concept (#5433).
func TestAdapterMapsCronJobNestedPodSpecOnlyNoReplicaStatus(t *testing.T) {
	t.Parallel()

	cronjob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Namespace: "batch", Name: "nightly-export-cron", UID: "uid-cj"},
		Spec: batchv1.CronJobSpec{
			Schedule: "0 2 * * *",
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "export", Image: "docker.io/myapp/export:v1"}},
						},
					},
				},
			},
		},
	}
	adapter := NewAdapter(fake.NewClientset(cronjob))

	result, err := adapter.ListCronJobs(context.Background())
	if err != nil {
		t.Fatalf("ListCronJobs() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("cronjob count = %d, want 1", len(result.Items))
	}
	workload := result.Items[0]
	if len(workload.Containers) != 1 || workload.Containers[0].Image != "docker.io/myapp/export:v1" {
		t.Fatalf("Containers = %+v, want one export container extracted from the nested job template", workload.Containers)
	}
	if workload.Meta.Resource != "cronjobs" || workload.Meta.APIGroup != "batch" {
		t.Fatalf("Meta = %+v, want APIGroup=batch Resource=cronjobs", workload.Meta)
	}
	if workload.DesiredReplicas != nil {
		t.Fatalf("DesiredReplicas = %v, want nil (CronJobs have no replica concept)", *workload.DesiredReplicas)
	}
	if workload.ReadyReplicas != nil {
		t.Fatalf("ReadyReplicas = %v, want nil (CronJobs have no replica concept)", *workload.ReadyReplicas)
	}
	if workload.AvailableReplicas != nil {
		t.Fatalf("AvailableReplicas = %v, want nil (CronJobs have no replica concept)", *workload.AvailableReplicas)
	}
	if workload.PodPhase != nil {
		t.Fatalf("PodPhase = %v, want nil (CronJobs carry no pod phase)", *workload.PodPhase)
	}
}
