// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// K8sSelectMatchInput carries the fields needed to evaluate the Service ->
// workload SELECTS relationship. The implementation moved to
// internal/query/querycontract (#6060) so the impact handler-family
// subpackages can match without importing this package; these aliases and
// wrappers keep root's own relationship builders, candidate readers, and
// tests compiling unchanged.
type k8sSelectMatchInput = querycontract.K8sSelectMatchInput

// k8sWorkloadMatchTarget is a workload prepared once for repeated SELECTS
// evaluation. See querycontract.K8sWorkloadMatchTarget.
type k8sWorkloadMatchTarget = querycontract.K8sWorkloadMatchTarget

const (
	// k8sSelectReasonNameNamespace marks a SELECTS edge inferred from
	// matching name+namespace. See querycontract.K8sSelectReasonNameNamespace.
	k8sSelectReasonNameNamespace = querycontract.K8sSelectReasonNameNamespace
	// k8sSelectReasonSelectorMatch marks a SELECTS edge proven by real
	// selector/pod-template-label matching. See
	// querycontract.K8sSelectReasonSelectorMatch.
	k8sSelectReasonSelectorMatch = querycontract.K8sSelectReasonSelectorMatch
)

// k8sSelectMatch evaluates whether service SELECTS workload. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func k8sSelectMatch(service, workload k8sSelectMatchInput) (matched bool, reason string, mixedVintageDrop bool) {
	return querycontract.K8sSelectMatch(service, workload)
}

// newK8sWorkloadMatchTarget prepares workload for repeated Match calls. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func newK8sWorkloadMatchTarget(workload k8sSelectMatchInput) k8sWorkloadMatchTarget {
	return querycontract.NewK8sWorkloadMatchTarget(workload)
}

// k8sSelectMatchInputFromEntity adapts an EntityContent row into
// k8sSelectMatchInput. The implementation moved to querycontract for #6060;
// this wrapper keeps root callers unchanged.
func k8sSelectMatchInputFromEntity(entity EntityContent) k8sSelectMatchInput {
	return querycontract.K8sSelectMatchInputFromEntity(entity)
}

// k8sSelectorSubsetOf reports whether every key=value pair in selector is
// present with an equal value in labels. The implementation moved to
// querycontract for #6060; this wrapper keeps root callers unchanged.
func k8sSelectorSubsetOf(selector, labels string) bool {
	return querycontract.K8sSelectorSubsetOf(selector, labels)
}
