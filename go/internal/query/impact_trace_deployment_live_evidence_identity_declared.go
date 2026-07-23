// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"sort"
	"strings"
)

// declaredObjectAnchorResourceByKind is the closed, collector-listed
// Kubernetes kind -> resource-plural mapping used to derive the expected
// group_version_resource for a declared kind+namespace+name identity anchor
// (#5639). It intentionally covers only the workload-shaped kinds the
// kubernetes_live.pod_template collector actually observes
// (go/internal/collector/kuberneteslive/clientgo): a kind outside this map
// produces NO anchor at all (declaredObjectAnchors fails closed) rather than
// guessing a resource plural that the collector never emits. Widening this
// map to a new kind requires confirming the collector's own informer/watch
// set actually lists that group/version/resource in a live cluster before it
// can ever legitimately match.
var declaredObjectAnchorResourceByKind = map[string]string{
	"Deployment":  "deployments",
	"ReplicaSet":  "replicasets",
	"StatefulSet": "statefulsets",
	"DaemonSet":   "daemonsets",
	"CronJob":     "cronjobs",
	"Job":         "jobs",
	"Pod":         "pods",
}

// apiVersionVersion derives the Kubernetes API version segment from a raw
// apiVersion string, the companion half of apiVersionGroup
// (impact_trace_deployment_live_evidence_identity.go): "group/version" names
// the version as the segment after the "/" ("apps/v1" -> "v1"), while a bare
// "version" ("v1") has no group segment, so the whole string IS the version.
func apiVersionVersion(apiVersion string) string {
	_, version, found := strings.Cut(apiVersion, "/")
	if !found {
		return apiVersion
	}
	return version
}

// declaredObjectAnchors computes the declared kind+namespace+name identity
// anchors (#5639) for every k8sResource whose kind is in the closed
// declaredObjectAnchorResourceByKind map and which carries a non-empty
// namespace and name. This is the WEAKER anchor family relative to an ArgoCD
// tracking-id (resolveLiveIdentityAnchors ranks it last): it binds directly
// to the resource's own declared kind, namespace, and name rather than an
// annotation ArgoCD writes, so it applies even when a workload has no GitOps
// controller at all (Helm-only or kubectl-applied workloads).
//
// Fail-closed rules, ALL required together, matching the #5639 design:
//   - non-empty namespace on the declared side -- a cluster-scoped or
//     unprojected declared object can never anchor a live match (no
//     cluster-scoped/wildcard match is ever allowed).
//   - non-empty name.
//   - a kind present in the closed resource map -- an unmappable kind
//     produces no anchor rather than guessing a resource plural.
//
// Because every returned anchor carries the resource's own namespace and
// name, two workloads that share a namespace, kind, and even an image digest
// (the #5471 codex P1 shape) can never produce the same anchor unless they
// also share the same declared name -- distinct declared identity always
// stays distinct.
func declaredObjectAnchors(k8sResources []map[string]any) []liveIdentityAnchor {
	seen := make(map[string]struct{}, len(k8sResources))
	anchors := make([]liveIdentityAnchor, 0, len(k8sResources))
	for _, resource := range k8sResources {
		kind := StringVal(resource, "kind")
		name := StringVal(resource, "entity_name")
		namespace := StringVal(resource, "namespace")
		if namespace == "" || name == "" {
			continue
		}
		resourcePlural, ok := declaredObjectAnchorResourceByKind[kind]
		if !ok {
			continue
		}
		apiVersion := StringVal(resource, "api_version")
		gvr := fmt.Sprintf("%s/%s/%s", apiVersionGroup(apiVersion), apiVersionVersion(apiVersion), resourcePlural)
		key := gvr + "|" + namespace + "|" + name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		anchors = append(anchors, liveIdentityAnchor{
			Kind:                 liveIdentityAnchorDeclaredObject,
			GroupVersionResource: gvr,
			Namespace:            namespace,
			Name:                 name,
		})
	}
	sort.Slice(anchors, func(i, j int) bool {
		if anchors[i].GroupVersionResource != anchors[j].GroupVersionResource {
			return anchors[i].GroupVersionResource < anchors[j].GroupVersionResource
		}
		if anchors[i].Namespace != anchors[j].Namespace {
			return anchors[i].Namespace < anchors[j].Namespace
		}
		return anchors[i].Name < anchors[j].Name
	})
	return anchors
}
