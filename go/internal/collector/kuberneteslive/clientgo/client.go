// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package clientgo

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/eshu-hq/eshu/go/internal/collector/kuberneteslive"
)

// defaultListPageSize bounds one List page so a large cluster does not return an
// unbounded response in a single call. The adapter follows the continue token
// until the API server reports the listing is complete.
const defaultListPageSize int64 = 200

// Adapter implements kuberneteslive.Client against a typed client-go clientset.
// It is read-only and metadata-only; it lists a fixed core resource set and
// maps typed objects into the collector's neutral views.
type Adapter struct {
	clientset kubernetes.Interface
	pageSize  int64
}

// NewAdapter builds a read-only adapter from a typed clientset.
func NewAdapter(clientset kubernetes.Interface) *Adapter {
	return &Adapter{clientset: clientset, pageSize: defaultListPageSize}
}

// PingReadOnly verifies read access with a single bounded namespace list.
func (a *Adapter) PingReadOnly(ctx context.Context) error {
	_, err := a.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		return fmt.Errorf("read-only ping (list namespaces): %w", err)
	}
	return nil
}

// ListNamespaces lists cluster namespaces.
func (a *Adapter) ListNamespaces(ctx context.Context) (kuberneteslive.ListResult[kuberneteslive.ObjectMeta], error) {
	var items []kuberneteslive.ObjectMeta
	partial, reason, err := a.paginate(ctx, func(opts metav1.ListOptions) (string, error) {
		list, err := a.clientset.CoreV1().Namespaces().List(ctx, opts)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			ns := &list.Items[i]
			items = append(items, objectMeta("", "v1", "namespaces", ns.ObjectMeta))
		}
		return list.Continue, nil
	})
	if err != nil {
		return kuberneteslive.ListResult[kuberneteslive.ObjectMeta]{}, err
	}
	return kuberneteslive.ListResult[kuberneteslive.ObjectMeta]{Items: items, Partial: partial, Reason: reason}, nil
}

// ListPods lists pods across all namespaces. For each pod, it reads
// pod.Status.ContainerStatuses and InitContainerStatuses to resolve the
// CRI-resolved digest (ImageID) for each container, normalized into the
// repo@sha256:<digest> form, and pod.Status.Phase to populate the OBSERVED
// runtime pod phase (issue #5431).
func (a *Adapter) ListPods(ctx context.Context) (kuberneteslive.ListResult[kuberneteslive.WorkloadObject], error) {
	var items []kuberneteslive.WorkloadObject
	partial, reason, err := a.paginate(ctx, func(opts metav1.ListOptions) (string, error) {
		list, err := a.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, opts)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			pod := &list.Items[i]
			workload := workloadFromPod(
				objectMeta("", "v1", "pods", pod.ObjectMeta),
				pod,
			)
			if phase := string(pod.Status.Phase); phase != "" {
				workload.PodPhase = &phase
			}
			items = append(items, workload)
		}
		return list.Continue, nil
	})
	if err != nil {
		return kuberneteslive.ListResult[kuberneteslive.WorkloadObject]{}, err
	}
	return kuberneteslive.ListResult[kuberneteslive.WorkloadObject]{Items: items, Partial: partial, Reason: reason}, nil
}

// ListDeployments lists deployments across all namespaces. For each
// deployment, it reads .Spec.Replicas (DESIRED) and
// .Status.ReadyReplicas/.Status.AvailableReplicas (OBSERVED) to populate the
// workload's runtime-status fields (issue #5431).
func (a *Adapter) ListDeployments(ctx context.Context) (kuberneteslive.ListResult[kuberneteslive.WorkloadObject], error) {
	var items []kuberneteslive.WorkloadObject
	partial, reason, err := a.paginate(ctx, func(opts metav1.ListOptions) (string, error) {
		list, err := a.clientset.AppsV1().Deployments(metav1.NamespaceAll).List(ctx, opts)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			d := &list.Items[i]
			workload := workloadFromPodSpec(
				objectMeta("apps", "v1", "deployments", d.ObjectMeta),
				d.Spec.Template.Spec, d.Spec.Selector,
			)
			workload.DesiredReplicas = d.Spec.Replicas
			readyReplicas := d.Status.ReadyReplicas
			workload.ReadyReplicas = &readyReplicas
			availableReplicas := d.Status.AvailableReplicas
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

// ListReplicaSets lists replicasets across all namespaces. For each
// replicaset, it reads .Spec.Replicas (DESIRED) and
// .Status.ReadyReplicas/.Status.AvailableReplicas (OBSERVED) to populate the
// workload's runtime-status fields (issue #5431).
func (a *Adapter) ListReplicaSets(ctx context.Context) (kuberneteslive.ListResult[kuberneteslive.WorkloadObject], error) {
	var items []kuberneteslive.WorkloadObject
	partial, reason, err := a.paginate(ctx, func(opts metav1.ListOptions) (string, error) {
		list, err := a.clientset.AppsV1().ReplicaSets(metav1.NamespaceAll).List(ctx, opts)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			rs := &list.Items[i]
			workload := workloadFromPodSpec(
				objectMeta("apps", "v1", "replicasets", rs.ObjectMeta),
				rs.Spec.Template.Spec, rs.Spec.Selector,
			)
			workload.DesiredReplicas = rs.Spec.Replicas
			readyReplicas := rs.Status.ReadyReplicas
			workload.ReadyReplicas = &readyReplicas
			availableReplicas := rs.Status.AvailableReplicas
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

// ListServices lists services across all namespaces.
func (a *Adapter) ListServices(ctx context.Context) (kuberneteslive.ListResult[kuberneteslive.ServiceObject], error) {
	var items []kuberneteslive.ServiceObject
	partial, reason, err := a.paginate(ctx, func(opts metav1.ListOptions) (string, error) {
		list, err := a.clientset.CoreV1().Services(metav1.NamespaceAll).List(ctx, opts)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			svc := &list.Items[i]
			items = append(items, kuberneteslive.ServiceObject{
				Meta:     objectMeta("", "v1", "services", svc.ObjectMeta),
				Selector: copyStringMap(svc.Spec.Selector),
			})
		}
		return list.Continue, nil
	})
	if err != nil {
		return kuberneteslive.ListResult[kuberneteslive.ServiceObject]{}, err
	}
	return kuberneteslive.ListResult[kuberneteslive.ServiceObject]{Items: items, Partial: partial, Reason: reason}, nil
}

// ListIngresses lists ingresses across all namespaces.
func (a *Adapter) ListIngresses(ctx context.Context) (kuberneteslive.ListResult[kuberneteslive.IngressObject], error) {
	var items []kuberneteslive.IngressObject
	partial, reason, err := a.paginate(ctx, func(opts metav1.ListOptions) (string, error) {
		list, err := a.clientset.NetworkingV1().Ingresses(metav1.NamespaceAll).List(ctx, opts)
		if err != nil {
			return "", err
		}
		for i := range list.Items {
			ing := &list.Items[i]
			items = append(items, kuberneteslive.IngressObject{
				Meta:            objectMeta("networking.k8s.io", "v1", "ingresses", ing.ObjectMeta),
				BackendServices: ingressBackendServices(ing),
			})
		}
		return list.Continue, nil
	})
	if err != nil {
		return kuberneteslive.ListResult[kuberneteslive.IngressObject]{}, err
	}
	return kuberneteslive.ListResult[kuberneteslive.IngressObject]{Items: items, Partial: partial, Reason: reason}, nil
}

// paginate drives one resource family's list to completion through continue
// tokens. A Forbidden error becomes a partial result (warning), not a hard
// failure, so one denied family does not abort the whole snapshot. A partial
// failure after some pages also degrades to partial.
func (a *Adapter) paginate(ctx context.Context, page func(metav1.ListOptions) (string, error)) (bool, string, error) {
	cont := ""
	pagesFetched := 0
	for {
		opts := metav1.ListOptions{Limit: a.pageSize, Continue: cont}
		next, err := page(opts)
		if err != nil {
			if apierrors.IsForbidden(err) {
				return true, kuberneteslive.WarningForbiddenResource, nil
			}
			if pagesFetched > 0 {
				// Some pages succeeded, then the listing failed mid-stream.
				return true, kuberneteslive.WarningPartialList, nil
			}
			return false, "", err
		}
		pagesFetched++
		if next == "" {
			return false, "", nil
		}
		cont = next
	}
}

func objectMeta(apiGroup, version, resource string, meta metav1.ObjectMeta) kuberneteslive.ObjectMeta {
	owners := make([]kuberneteslive.OwnerReference, 0, len(meta.OwnerReferences))
	for _, owner := range meta.OwnerReferences {
		owners = append(owners, kuberneteslive.OwnerReference{
			APIVersion: owner.APIVersion,
			Kind:       owner.Kind,
			Name:       owner.Name,
			UID:        string(owner.UID),
		})
	}
	return kuberneteslive.ObjectMeta{
		APIGroup:        apiGroup,
		Version:         version,
		Resource:        resource,
		Namespace:       meta.Namespace,
		Name:            meta.Name,
		UID:             string(meta.UID),
		ResourceVersion: meta.ResourceVersion,
		Labels:          copyStringMap(meta.Labels),
		Annotations:     filterIdentityAnnotations(meta.Annotations),
		OwnerReferences: owners,
	}
}

// workloadFromPodSpec maps a pod spec into the neutral, metadata-only workload
// view. It extracts image refs, declared ports, env var NAMES, and service
// account. It never copies env var values, secret refs resolved to values, or
// any data payload.
func workloadFromPodSpec(meta kuberneteslive.ObjectMeta, spec corev1.PodSpec, selector *metav1.LabelSelector) kuberneteslive.WorkloadObject {
	containers := make([]kuberneteslive.ContainerSummary, 0, len(spec.Containers)+len(spec.InitContainers))
	for i := range spec.InitContainers {
		containers = append(containers, containerSummary(&spec.InitContainers[i], true))
	}
	for i := range spec.Containers {
		containers = append(containers, containerSummary(&spec.Containers[i], false))
	}
	return kuberneteslive.WorkloadObject{
		Meta:                         meta,
		ServiceAccount:               spec.ServiceAccountName,
		ProjectedServiceAccountToken: projectedServiceAccountToken(spec.Volumes),
		Selector:                     selectorLabels(selector),
		Containers:                   containers,
	}
}

// workloadFromPod maps a full Pod object into the neutral workload view. Only
// Pods carry container statuses, so this function reads
// pod.Status.ContainerStatuses and pod.Status.InitContainerStatuses to resolve
// the CRI-resolved digest (ImageID) for each container, normalized via
// kuberneteslive.NormalizeCRIImageID. Deployments and ReplicaSets carry only
// the pod template spec (no status), so they use workloadFromPodSpec instead.
func workloadFromPod(meta kuberneteslive.ObjectMeta, pod *corev1.Pod) kuberneteslive.WorkloadObject {
	workload := workloadFromPodSpec(meta, pod.Spec, nil)
	// Build a name->normalized-digest index from container statuses.
	statusDigests := make(map[string]string, len(pod.Status.ContainerStatuses)+len(pod.Status.InitContainerStatuses))
	for i := range pod.Status.ContainerStatuses {
		cs := &pod.Status.ContainerStatuses[i]
		if digest := kuberneteslive.NormalizeCRIImageID(cs.ImageID); digest != "" {
			statusDigests[cs.Name] = digest
		}
	}
	for i := range pod.Status.InitContainerStatuses {
		cs := &pod.Status.InitContainerStatuses[i]
		if digest := kuberneteslive.NormalizeCRIImageID(cs.ImageID); digest != "" {
			statusDigests[cs.Name] = digest
		}
	}
	if len(statusDigests) == 0 {
		return workload
	}
	// Patch resolved digests onto the corresponding spec containers by name.
	for i := range workload.Containers {
		name := workload.Containers[i].Name
		if digest, ok := statusDigests[name]; ok {
			workload.Containers[i].ResolvedImageDigest = digest
		}
	}
	return workload
}

func containerSummary(container *corev1.Container, init bool) kuberneteslive.ContainerSummary {
	ports := make([]int32, 0, len(container.Ports))
	for _, port := range container.Ports {
		ports = append(ports, port.ContainerPort)
	}
	envKeys := make([]string, 0, len(container.Env))
	envFromSecret := false
	for _, env := range container.Env {
		// Record only the variable NAME. Never read env.Value, and never read a
		// secret/configmap reference's underlying value.
		envKeys = append(envKeys, env.Name)
		if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
			envFromSecret = true
		}
	}
	for _, envFrom := range container.EnvFrom {
		if envFrom.SecretRef != nil {
			envFromSecret = true
		}
	}
	return kuberneteslive.ContainerSummary{
		Name:          container.Name,
		Image:         container.Image,
		Init:          init,
		Ports:         ports,
		EnvKeys:       envKeys,
		EnvFromSecret: envFromSecret,
	}
}

func ingressBackendServices(ing *networkingv1.Ingress) []string {
	seen := make(map[string]struct{})
	var services []string
	add := func(backend *networkingv1.IngressBackend) {
		if backend == nil || backend.Service == nil {
			return
		}
		name := backend.Service.Name
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		services = append(services, name)
	}
	add(ing.Spec.DefaultBackend)
	for ri := range ing.Spec.Rules {
		rule := &ing.Spec.Rules[ri]
		if rule.HTTP == nil {
			continue
		}
		for pi := range rule.HTTP.Paths {
			add(&rule.HTTP.Paths[pi].Backend)
		}
	}
	return services
}

func selectorLabels(selector *metav1.LabelSelector) map[string]string {
	if selector == nil {
		return nil
	}
	return copyStringMap(selector.MatchLabels)
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

// identityAnnotationAllowlist is the fixed set of ObjectMeta annotation keys
// the collector captures into a workload's kubernetes_live.pod_template fact,
// via filterIdentityAnnotations. Kubernetes annotation values are unbounded —
// kubectl.kubernetes.io/last-applied-configuration is commonly the full
// applied manifest re-serialized as JSON (KB to tens of KB) and can embed
// Secret data verbatim when the applied object was a Secret — so copying the
// full ObjectMeta.Annotations map into every collected workload would make
// annotation capture an unbounded, potentially secret-leaking cost on this
// hot collection path. The design intent (#5471 F2) is narrow: surface only
// the declared->live identity-binding signals the reducer's
// BINDS_LIVE_WORKLOAD correlation needs — the ArgoCD app-instance tracking
// annotation and the Kustomize/Helm app.kubernetes.io instance/name
// convention. Nothing else is a known consumer.
var identityAnnotationAllowlist = map[string]struct{}{
	"argocd.argoproj.io/tracking-id": {},
	"app.kubernetes.io/instance":     {},
	"app.kubernetes.io/name":         {},
}

// filterIdentityAnnotations returns a copy of input restricted to
// identityAnnotationAllowlist. It is the single point where an object's
// annotations are captured for the kubernetes_live.pod_template fact, so the
// unbounded full ObjectMeta.Annotations map never reaches a fact payload; see
// identityAnnotationAllowlist for why. It returns nil, never an empty map,
// when input has no allowlisted key — this must decode identically to "no
// annotations observed" for backward compatibility.
func filterIdentityAnnotations(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	var output map[string]string
	for key, value := range input {
		if _, ok := identityAnnotationAllowlist[key]; !ok {
			continue
		}
		if output == nil {
			output = make(map[string]string, len(identityAnnotationAllowlist))
		}
		output[key] = value
	}
	return output
}
