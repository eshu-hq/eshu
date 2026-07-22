// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"sort"
	"strings"
)

// expectedArgoCDTrackingIDsQueryLimit caps how many expected ArgoCD
// tracking-id values fetchWorkloadLiveEvidence queries the pod-template
// store for. controllers and k8sResources reaching expectedArgoCDTrackingIDs
// are already capped at serviceStoryItemLimit each
// (impact_trace_deployment_gitops_helpers.go, impact_trace_deployment_resources.go),
// so this defensive cap keeps the app-name x resource cross product bounded
// even if an upstream cap ever changes.
const expectedArgoCDTrackingIDsQueryLimit = serviceStoryItemLimit

// expectedArgoCDTrackingIDs computes the set of ArgoCD annotation-based
// tracking-id values (argocd.argoproj.io/tracking-id) the traced workload's
// live Kubernetes object(s) would carry, derived entirely from
// DECLARED/config-side evidence: the traced workload's own ArgoCD
// Application controller(s) (controllers, already filtered to the traced
// workload by selectRelevantDeploymentSourceControllers) and its own
// declared k8sResources.
//
// The tracking-id format is ArgoCD's own annotation convention
// (BuildAppInstanceValue, argoproj/argo-cd util/argo/resource_tracking.go):
// "<app-name>:<group>/<kind>:<namespace>/<name>". Because controllers and
// k8sResources are already scoped to the single traced workload, the
// app-name x resource cross product this function computes cannot leak
// another workload's identity, even when two workloads share a GitOps
// config repo or an image digest (#5471 codex P1).
//
// Returns an empty, nil set when there is no argocd_application controller
// or no k8sResource carries a computable kind+name -- fetchWorkloadLiveEvidence
// treats an empty set as "no ArgoCD identity to bind live evidence to" and
// fails closed to config_only WITHOUT querying the pod-template store, which
// is the core fix: a shared image digest alone can never promote a workload
// that has no resolvable declared identity.
func expectedArgoCDTrackingIDs(controllers []map[string]any, k8sResources []map[string]any) []string {
	appNames := argoCDApplicationNames(controllers)
	if len(appNames) == 0 || len(k8sResources) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(appNames)*len(k8sResources))
	trackingIDs := make([]string, 0, len(appNames)*len(k8sResources))
	for _, resource := range k8sResources {
		kind := StringVal(resource, "kind")
		name := StringVal(resource, "entity_name")
		if kind == "" || name == "" {
			continue
		}
		group := apiVersionGroup(StringVal(resource, "api_version"))
		namespace := StringVal(resource, "namespace")
		for _, appName := range appNames {
			trackingID := buildArgoCDTrackingID(appName, group, kind, namespace, name)
			if _, ok := seen[trackingID]; ok {
				continue
			}
			seen[trackingID] = struct{}{}
			trackingIDs = append(trackingIDs, trackingID)
		}
	}
	sort.Strings(trackingIDs)
	if len(trackingIDs) > expectedArgoCDTrackingIDsQueryLimit {
		trackingIDs = trackingIDs[:expectedArgoCDTrackingIDsQueryLimit]
	}
	return trackingIDs
}

// argoCDApplicationNames returns the deduplicated ArgoCD Application names
// declared by controllers whose controller_kind is "argocd_application"
// (buildDeploymentSourceControllerEntity,
// impact_trace_deployment_gitops_helpers.go). An ApplicationSet or Flux
// controller carries no ArgoCD annotation-based tracking-id, so it is
// deliberately excluded here.
func argoCDApplicationNames(controllers []map[string]any) []string {
	seen := make(map[string]struct{}, len(controllers))
	names := make([]string, 0, len(controllers))
	for _, controller := range controllers {
		if StringVal(controller, "controller_kind") != "argocd_application" {
			continue
		}
		name := StringVal(controller, "entity_name")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

// buildArgoCDTrackingID formats one ArgoCD annotation-based tracking-id
// value using ArgoCD's own BuildAppInstanceValue convention
// (argoproj/argo-cd util/argo/resource_tracking.go):
// "%s:%s/%s:%s/%s" applied to (appName, group, kind, namespace, name).
// ArgoCD's own format string does not special-case an empty group or
// namespace, so the Kubernetes core API group ("v1", group "") leaves an
// empty segment before the "/" (e.g. "myapp:/Service:default/my-svc"), and a
// cluster-scoped resource similarly leaves an empty namespace segment
// (e.g. "myapp:apps/ClusterRole:/my-role").
func buildArgoCDTrackingID(appName, group, kind, namespace, name string) string {
	return fmt.Sprintf("%s:%s/%s:%s/%s", appName, group, kind, namespace, name)
}

// apiVersionGroup derives the Kubernetes API group from a raw apiVersion
// string, per the Kubernetes API conventions: "group/version" names a
// group ("apps/v1" -> "apps"), while a bare "version" ("v1") names the core
// group, which has no group segment and therefore returns "".
func apiVersionGroup(apiVersion string) string {
	group, _, found := strings.Cut(apiVersion, "/")
	if !found {
		return ""
	}
	return group
}
