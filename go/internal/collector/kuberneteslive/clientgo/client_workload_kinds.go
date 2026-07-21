// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package clientgo

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/eshu-hq/eshu/go/internal/collector/kuberneteslive"
)

// ListStatefulSets lists statefulsets across all namespaces. For each
// statefulset, it reads .Spec.Replicas (DESIRED) and
// .Status.ReadyReplicas/.Status.AvailableReplicas (OBSERVED) to populate the
// workload's runtime-status fields, mirroring the Deployment/ReplicaSet
// mapping (#5433).
func (a *Adapter) ListStatefulSets(ctx context.Context) (kuberneteslive.ListResult[kuberneteslive.WorkloadObject], error) {
	var items []kuberneteslive.WorkloadObject
	partial, reason, err := a.paginate(ctx, func(opts metav1.ListOptions) (string, error) {
		list, err := a.clientset.AppsV1().StatefulSets(metav1.NamespaceAll).List(ctx, opts)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			ss := &list.Items[i]
			workload := workloadFromPodSpec(
				objectMeta("apps", "v1", "statefulsets", ss.ObjectMeta),
				ss.Spec.Template.Spec, ss.Spec.Selector,
			)
			workload.DesiredReplicas = ss.Spec.Replicas
			readyReplicas := ss.Status.ReadyReplicas
			workload.ReadyReplicas = &readyReplicas
			availableReplicas := ss.Status.AvailableReplicas
			workload.AvailableReplicas = &availableReplicas
			items = append(items, workload)
		}
		return list.Continue, nil
	})
	if err != nil {
		return kuberneteslive.ListResult[kuberneteslive.WorkloadObject]{}, err
	}
	return kuberneteslive.ListResult[kuberneteslive.WorkloadObject]{Items: items, Partial: partial, Reason: reason}, nil
}

// ListDaemonSets lists daemonsets across all namespaces. DaemonSetStatus has
// no replica-count fields (a DaemonSet is scheduled per-node, not scaled to a
// replica count), so this maps its per-node scheduling counts as the
// workload's replica-equivalent on an OBSERVED basis only:
// DesiredReplicas <- .Status.DesiredNumberScheduled, ReadyReplicas <-
// .Status.NumberReady, AvailableReplicas <- .Status.NumberAvailable. These are
// NOT literal apps/v1 ReplicaSet-style fields; they are the closest DaemonSet
// analogue and are documented here so a reader of the emitted fact does not
// mistake them for a replica spec (#5433).
func (a *Adapter) ListDaemonSets(ctx context.Context) (kuberneteslive.ListResult[kuberneteslive.WorkloadObject], error) {
	var items []kuberneteslive.WorkloadObject
	partial, reason, err := a.paginate(ctx, func(opts metav1.ListOptions) (string, error) {
		list, err := a.clientset.AppsV1().DaemonSets(metav1.NamespaceAll).List(ctx, opts)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			ds := &list.Items[i]
			workload := workloadFromPodSpec(
				objectMeta("apps", "v1", "daemonsets", ds.ObjectMeta),
				ds.Spec.Template.Spec, ds.Spec.Selector,
			)
			desiredScheduled := ds.Status.DesiredNumberScheduled
			workload.DesiredReplicas = &desiredScheduled
			numberReady := ds.Status.NumberReady
			workload.ReadyReplicas = &numberReady
			numberAvailable := ds.Status.NumberAvailable
			workload.AvailableReplicas = &numberAvailable
			items = append(items, workload)
		}
		return list.Continue, nil
	})
	if err != nil {
		return kuberneteslive.ListResult[kuberneteslive.WorkloadObject]{}, err
	}
	return kuberneteslive.ListResult[kuberneteslive.WorkloadObject]{Items: items, Partial: partial, Reason: reason}, nil
}

// ListJobs lists jobs across all namespaces. A Job has no replica concept (it
// runs completions to success, not a steady-state replica count), so all
// runtime-status fields (DesiredReplicas, ReadyReplicas, AvailableReplicas,
// PodPhase) are left nil. Job status (Active/Succeeded/Failed counts) is a
// different shape not carried by pod_template; only the pod template spec is
// emitted (#5433).
func (a *Adapter) ListJobs(ctx context.Context) (kuberneteslive.ListResult[kuberneteslive.WorkloadObject], error) {
	var items []kuberneteslive.WorkloadObject
	partial, reason, err := a.paginate(ctx, func(opts metav1.ListOptions) (string, error) {
		list, err := a.clientset.BatchV1().Jobs(metav1.NamespaceAll).List(ctx, opts)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			job := &list.Items[i]
			workload := workloadFromPodSpec(
				objectMeta("batch", "v1", "jobs", job.ObjectMeta),
				job.Spec.Template.Spec, job.Spec.Selector,
			)
			items = append(items, workload)
		}
		return list.Continue, nil
	})
	if err != nil {
		return kuberneteslive.ListResult[kuberneteslive.WorkloadObject]{}, err
	}
	return kuberneteslive.ListResult[kuberneteslive.WorkloadObject]{Items: items, Partial: partial, Reason: reason}, nil
}

// ListCronJobs lists cronjobs across all namespaces. The pod template spec is
// nested under .Spec.JobTemplate.Spec.Template.Spec rather than directly under
// .Spec.Template.Spec like the other workload kinds. Like Job, a CronJob has
// no replica concept, so all runtime-status fields are left nil (#5433).
func (a *Adapter) ListCronJobs(ctx context.Context) (kuberneteslive.ListResult[kuberneteslive.WorkloadObject], error) {
	var items []kuberneteslive.WorkloadObject
	partial, reason, err := a.paginate(ctx, func(opts metav1.ListOptions) (string, error) {
		list, err := a.clientset.BatchV1().CronJobs(metav1.NamespaceAll).List(ctx, opts)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			cj := &list.Items[i]
			jobSpec := cj.Spec.JobTemplate.Spec
			workload := workloadFromPodSpec(
				objectMeta("batch", "v1", "cronjobs", cj.ObjectMeta),
				jobSpec.Template.Spec, jobSpec.Selector,
			)
			items = append(items, workload)
		}
		return list.Continue, nil
	})
	if err != nil {
		return kuberneteslive.ListResult[kuberneteslive.WorkloadObject]{}, err
	}
	return kuberneteslive.ListResult[kuberneteslive.WorkloadObject]{Items: items, Partial: partial, Reason: reason}, nil
}
