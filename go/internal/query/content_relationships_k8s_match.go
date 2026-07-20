// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

// Relationship reasons for the k8s Service->workload SELECTS edge. Both are
// truth labels surfaced on the wire under relationship["reason"]; keep them
// registered in docs/public/languages/kubernetes.md if that changes.
const (
	// k8sSelectReasonNameNamespace marks a SELECTS edge inferred from
	// matching name+namespace because the Service's selector state is
	// unknown (pre-upgrade content row -- see k8sSelectMatchInput.selectorPresent).
	k8sSelectReasonNameNamespace = "k8s_service_name_namespace"
	// k8sSelectReasonSelectorMatch marks a SELECTS edge proven by real
	// selector/pod-template-label matching: the Service's spec.selector is a
	// known, non-empty subset of the workload's pod-template labels.
	k8sSelectReasonSelectorMatch = "k8s_service_selector_match"
)

// k8sSelectMatchInput carries the fields needed to evaluate the Service ->
// workload SELECTS relationship, independent of whether the caller holds an
// EntityContent row (content_relationships.go) or a flattened
// map[string]any row (impact_trace_deployment_k8s.go). selectorPresent and
// podTemplateLabelsPresent distinguish "key absent" (pre-upgrade data, truth
// unknown) from "key present but empty" (a known, empty value) -- the
// tri-state distinction the matcher depends on.
type k8sSelectMatchInput struct {
	kind                     string
	name                     string
	namespace                string
	selector                 string
	selectorPresent          bool
	podTemplateLabels        string
	podTemplateLabelsPresent bool
}

// k8sSelectMatch evaluates whether service SELECTS workload, and if so, by
// which reason. The v1 matcher scope is Deployment-only; other pod-template
// kinds are captured by the parser (see semantics.go) but not yet matched
// here, so this makes no new capability claim.
//
// Tri-state selector semantics (the anti-false-positive-masking core):
//
//   - selector key ABSENT (selectorPresent == false): selector truth is
//     unknown (pre-upgrade content row). Falls back to name+namespace
//     matching, reason k8sSelectReasonNameNamespace.
//   - selector key PRESENT and EMPTY: a genuinely selectorless Service
//     (ExternalName, manual Endpoints). No edge, no fallback -- an empty
//     selector must never vacuously match every workload.
//   - selector key PRESENT and NON-EMPTY: authoritative. If the selector is
//     a subset of the workload's pod-template labels, SELECTS with reason
//     k8sSelectReasonSelectorMatch; otherwise NO edge and NO fallback. The
//     name fallback is structurally unreachable once the selector is known,
//     which is what stops a stale/wrong selector from being masked by the
//     name+namespace heuristic.
//
// Mixed vintage (Service selector known, but the workload row predates
// pod_template_labels capture -- podTemplateLabelsPresent == false) also
// produces no match and no fallback: a transient false negative until
// re-ingest, preferred over guessing. mixedVintageDrop reports this specific
// case so a caller can log it as an operator diagnostic (see
// buildOutgoingK8sSelectRelationships / buildIncomingK8sSelectRelationships,
// content_relationships.go): it self-heals on re-ingest, so it is a Debug
// signal, not a Warn.
//
// Namespace scoping is strict in both the fallback and authoritative cases.
//
// k8sSelectMatch is the shared single-call entry point used by the
// entity-context relationship builders (content_relationships_k8s.go) and the
// impact-trace all-pairs builder (impact_trace_deployment_k8s.go). Its
// signature and behavior are frozen: callers that evaluate one Service against
// one workload rely on it unchanged. It is now a thin wrapper that constructs
// a k8sWorkloadMatchTarget and delegates to the single tri-state decision tree
// in k8sWorkloadMatchTarget.Match, so there is exactly one implementation of
// the tri-state semantics. A caller that evaluates MANY Services against the
// SAME workload (the #5363 impact-trace directed candidate scan) must build one
// k8sWorkloadMatchTarget per workload and reuse it, because the target parses
// the workload's pod-template labels ONCE; calling k8sSelectMatch per candidate
// re-parses that label map on every call (measured 16.13 ms/op vs 5.57 ms/op on
// a 5000-candidate worst case -- see evidence-5363-impact-trace-k8s-fetch.md).
func k8sSelectMatch(service, workload k8sSelectMatchInput) (matched bool, reason string, mixedVintageDrop bool) {
	return newK8sWorkloadMatchTarget(workload).Match(service)
}

// k8sWorkloadMatchTarget is a workload (Deployment) prepared once for repeated
// SELECTS evaluation against many candidate Services. It holds the workload's
// k8sSelectMatchInput plus its pod-template labels parsed a single time into a
// map, so a directed scan of N candidate Services against one workload parses
// the workload label map once instead of N times. parsedPodTemplateLabels is
// nil when the workload row carries no pod_template_labels key
// (podTemplateLabelsPresent == false), which the tri-state decision tree in
// Match treats as the mixed-vintage case, exactly as the pre-refactor
// k8sSelectMatch did.
type k8sWorkloadMatchTarget struct {
	workload                k8sSelectMatchInput
	parsedPodTemplateLabels map[string]string
}

// newK8sWorkloadMatchTarget prepares workload for repeated Match calls, parsing
// its pod-template labels once. The parse is skipped entirely when the workload
// row predates pod_template_labels capture (podTemplateLabelsPresent == false),
// so a mixed-vintage workload allocates no label map.
func newK8sWorkloadMatchTarget(workload k8sSelectMatchInput) k8sWorkloadMatchTarget {
	var parsed map[string]string
	if workload.podTemplateLabelsPresent {
		parsed = parseK8sLabelPairs(workload.podTemplateLabels)
	}
	return k8sWorkloadMatchTarget{workload: workload, parsedPodTemplateLabels: parsed}
}

// Match evaluates whether service SELECTS this prepared workload, returning the
// same (matched, reason, mixedVintageDrop) triple as the historical
// k8sSelectMatch. This is the single tri-state decision tree; see the
// k8sSelectMatch doc comment above for the full selector-present/empty/absent
// and mixed-vintage semantics. Behavior is byte-for-byte identical to the
// pre-#5363 k8sSelectMatch body: the only change is that the workload's
// pod-template labels come from the once-parsed t.parsedPodTemplateLabels
// instead of being re-parsed from the raw string on every call.
func (t k8sWorkloadMatchTarget) Match(service k8sSelectMatchInput) (matched bool, reason string, mixedVintageDrop bool) {
	if !strings.EqualFold(t.workload.kind, "Deployment") {
		return false, "", false
	}
	if !strings.EqualFold(service.namespace, t.workload.namespace) {
		return false, "", false
	}

	if !service.selectorPresent {
		if service.name != "" && service.name == t.workload.name {
			return true, k8sSelectReasonNameNamespace, false
		}
		return false, "", false
	}

	if service.selector == "" {
		return false, "", false
	}

	if !t.workload.podTemplateLabelsPresent {
		return false, "", true
	}

	if k8sSelectorSubsetOfParsed(service.selector, t.parsedPodTemplateLabels) {
		return true, k8sSelectReasonSelectorMatch, false
	}
	return false, "", false
}

// k8sSelectorSubsetOf reports whether every key=value pair in selector
// (Eshu's sorted "k=v,k=v" encoding) is present with an equal value in
// labels. An empty selector is never a subset of anything -- callers must
// gate on a non-empty, known selector before calling this (see
// k8sSelectMatch); this guard exists so the emptiness rule holds even if a
// future caller forgets.
func k8sSelectorSubsetOf(selector, labels string) bool {
	if selector == "" {
		return false
	}
	return k8sSelectorSubsetOfParsed(selector, parseK8sLabelPairs(labels))
}

// k8sSelectorSubsetOfParsed is k8sSelectorSubsetOf with the label side already
// parsed, so a directed scan of many selectors against one prepared workload
// (k8sWorkloadMatchTarget.Match) parses the workload label map once. An empty
// selector is never a subset of anything (the emptiness rule, guarded here as
// well as by the caller). labelPairs may be nil (a workload whose
// pod_template_labels key is present but empty): a non-empty selector then has
// no matching pair and is correctly not a subset.
func k8sSelectorSubsetOfParsed(selector string, labelPairs map[string]string) bool {
	if selector == "" {
		return false
	}
	for key, value := range parseK8sLabelPairs(selector) {
		if labelPairs[key] != value {
			return false
		}
	}
	return true
}

// parseK8sLabelPairs decodes Eshu's sorted "k=v,k=v" label encoding (see
// collectLabelLikeMap in internal/parser/yaml/semantics.go) into a map.
func parseK8sLabelPairs(encoded string) map[string]string {
	if encoded == "" {
		return nil
	}
	segments := strings.Split(encoded, ",")
	pairs := make(map[string]string, len(segments))
	for _, segment := range segments {
		key, value, ok := strings.Cut(segment, "=")
		if !ok {
			continue
		}
		pairs[key] = value
	}
	return pairs
}

// k8sSelectMatchInputFromEntity adapts an EntityContent row (the shape used
// by content_relationships.go) into k8sSelectMatchInput.
func k8sSelectMatchInputFromEntity(entity EntityContent) k8sSelectMatchInput {
	kind, _ := entity.Metadata["kind"].(string)
	selector, selectorPresent := entity.Metadata["selector"].(string)
	podTemplateLabels, podTemplateLabelsPresent := entity.Metadata["pod_template_labels"].(string)
	return k8sSelectMatchInput{
		kind:                     kind,
		name:                     entity.EntityName,
		namespace:                k8sNamespace(entity.Metadata),
		selector:                 selector,
		selectorPresent:          selectorPresent,
		podTemplateLabels:        podTemplateLabels,
		podTemplateLabelsPresent: podTemplateLabelsPresent,
	}
}

// k8sSelectMatchInputFromRow adapts a flattened map[string]any row (the
// shape used by impact_trace_deployment_k8s.go and its resource builders)
// into k8sSelectMatchInput. Presence of the "selector"/"pod_template_labels"
// keys in the row carries the same tri-state meaning as the metadata map
// keys on EntityContent -- callers that build these rows must omit the key
// entirely rather than set it to a zero value when the source data lacks it.
func k8sSelectMatchInputFromRow(row map[string]any) k8sSelectMatchInput {
	selector, selectorPresent := row["selector"].(string)
	podTemplateLabels, podTemplateLabelsPresent := row["pod_template_labels"].(string)
	return k8sSelectMatchInput{
		kind:                     safeStr(row, "kind"),
		name:                     safeStr(row, "entity_name"),
		namespace:                safeStr(row, "namespace"),
		selector:                 selector,
		selectorPresent:          selectorPresent,
		podTemplateLabels:        podTemplateLabels,
		podTemplateLabelsPresent: podTemplateLabelsPresent,
	}
}
