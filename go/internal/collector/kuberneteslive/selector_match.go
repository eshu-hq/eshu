// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kuberneteslive

import "context"

// matchSelectors emits one kubernetes_live.relationship selector_match fact
// for every Service whose label selector is a subset of a Pod's labels within
// the same namespace (issue #5437). It runs after collectWorkloads so
// b.podLabelIndex is fully populated, and before collectIngresses (order
// relative to ingresses does not matter; this keeps run() grouped by
// dependency: Services and Pods must both be indexed first).
//
// The pass is bucketed by namespace: b.podLabelIndex is a map keyed by
// namespace (builder.go), so for each Service this looks up only the Pods in
// that Service's namespace instead of scanning every Pod in the cluster and
// discarding cross-namespace pairs — O(services x pods-in-namespace) rather
// than O(services x all-pods). Iteration order is deterministic: services are
// visited in list order (b.serviceSelectorIndex is an ordered slice), and
// within each service the matched namespace's pods are visited in list order
// (each b.podLabelIndex bucket retains insertion order), which is a stable
// subsequence of the pre-bucketing full-scan order.
func (b *generationBuilder) matchSelectors(ctx context.Context) error {
	for _, service := range b.serviceSelectorIndex {
		for _, pod := range b.podLabelIndex[service.identity.Namespace] {
			if !selectorMatchesLabels(service.selector, pod.labels) {
				continue
			}
			envelope, err := NewRelationshipEnvelope(RelationshipObservation{
				ClusterID:           b.target.ClusterID,
				Type:                RelationshipSelectorMatch,
				From:                service.identity,
				To:                  pod.identity,
				GenerationID:        b.generationID(),
				CollectorInstanceID: b.collectorInstanceID,
				FencingToken:        b.target.FencingToken,
				ObservedAt:          b.observedAt,
				SourceURI:           b.target.SourceURI,
			})
			if err != nil {
				return err
			}
			b.append(ctx, envelope)
		}
	}
	return nil
}

// selectorMatchesLabels reports whether every key=value pair in selector is
// present in labels. An empty selector always returns false: Kubernetes never
// treats an empty Service selector as "match every Pod" (an empty-selector
// Service is either headless with manually managed Endpoints, or
// misconfigured), so this collector never emits a selector_match edge for one.
func selectorMatchesLabels(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}
