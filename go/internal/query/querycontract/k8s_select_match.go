// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"log/slog"
	"strings"
)

// Relationship reasons for the k8s Service->workload SELECTS edge. Both are
// truth labels surfaced on the wire under relationship["reason"]; keep them
// registered in docs/public/languages/kubernetes.md if that changes.
const (
	// K8sSelectReasonNameNamespace marks a SELECTS edge inferred from
	// matching name+namespace because the Service's selector state is
	// unknown (pre-upgrade content row -- see K8sSelectMatchInput.SelectorPresent).
	K8sSelectReasonNameNamespace = "k8s_service_name_namespace"
	// K8sSelectReasonSelectorMatch marks a SELECTS edge proven by real
	// selector/pod-template-label matching: the Service's spec.Selector is a
	// known, non-empty subset of the workload's pod-template labels.
	K8sSelectReasonSelectorMatch = "k8s_service_selector_match"
)

// K8sSelectMatchInput carries the fields needed to evaluate the Service ->
// workload SELECTS relationship, independent of whether the caller holds an
// EntityContent row (content_relationships.go) or a flattened
// map[string]any row (impact_trace_deployment_k8s.go). selectorPresent and
// podTemplateLabelsPresent distinguish "key absent" (pre-upgrade data, truth
// unknown) from "key present but empty" (a known, empty value) -- the
// tri-state distinction the matcher depends on.
type K8sSelectMatchInput struct {
	Kind                     string
	Name                     string
	Namespace                string
	Selector                 string
	SelectorPresent          bool
	PodTemplateLabels        string
	PodTemplateLabelsPresent bool
}

// K8sSelectMatch evaluates whether service SELECTS workload, and if so, by
// which reason. The v1 matcher scope is Deployment-only; other pod-template
// kinds are captured by the parser (see semantics.go) but not yet matched
// here, so this makes no new capability claim.
//
// Tri-state selector semantics (the anti-false-positive-masking core):
//
//   - selector key ABSENT (selectorPresent == false): selector truth is
//     unknown (pre-upgrade content row). Falls back to name+namespace
//     matching, reason K8sSelectReasonNameNamespace.
//   - selector key PRESENT and EMPTY: a genuinely selectorless Service
//     (ExternalName, manual Endpoints). No edge, no fallback -- an empty
//     selector must never vacuously match every workload.
//   - selector key PRESENT and NON-EMPTY: authoritative. If the selector is
//     a subset of the workload's pod-template labels, SELECTS with reason
//     K8sSelectReasonSelectorMatch; otherwise NO edge and NO fallback. The
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
// a K8sWorkloadMatchTarget and delegates to the single tri-state decision tree
// in K8sWorkloadMatchTarget.Match, so there is exactly one implementation of
// the tri-state semantics. A caller that evaluates MANY Services against the
// SAME workload (the #5363 impact-trace directed candidate scan) must build one
// K8sWorkloadMatchTarget per workload and reuse it, because the target parses
// the workload's pod-template labels ONCE; calling k8sSelectMatch per candidate
// re-parses that label map on every call (measured 16.13 ms/op vs 5.57 ms/op on
// a 5000-candidate worst case -- see evidence-5363-impact-trace-k8s-fetch.md).
func K8sSelectMatch(service, workload K8sSelectMatchInput) (matched bool, reason string, mixedVintageDrop bool) {
	return NewK8sWorkloadMatchTarget(workload).Match(service)
}

// K8sWorkloadMatchTarget is a workload (Deployment) prepared once for repeated
// SELECTS evaluation against many candidate Services. It holds the workload's
// K8sSelectMatchInput plus its pod-template labels parsed a single time into a
// map, so a directed scan of N candidate Services against one workload parses
// the workload label map once instead of N times. parsedPodTemplateLabels is
// nil when the workload row carries no pod_template_labels key
// (podTemplateLabelsPresent == false), which the tri-state decision tree in
// Match treats as the mixed-vintage case, exactly as the pre-refactor
// k8sSelectMatch did.
type K8sWorkloadMatchTarget struct {
	Workload                K8sSelectMatchInput
	ParsedPodTemplateLabels map[string]string
}

// NewK8sWorkloadMatchTarget prepares workload for repeated Match calls, parsing
// its pod-template labels once. The parse is skipped entirely when the workload
// row predates pod_template_labels capture (podTemplateLabelsPresent == false),
// so a mixed-vintage workload allocates no label map.
func NewK8sWorkloadMatchTarget(workload K8sSelectMatchInput) K8sWorkloadMatchTarget {
	var parsed map[string]string
	if workload.PodTemplateLabelsPresent {
		parsed = parseK8sLabelPairs(workload.PodTemplateLabels)
	}
	return K8sWorkloadMatchTarget{Workload: workload, ParsedPodTemplateLabels: parsed}
}

// Match evaluates whether service SELECTS this prepared workload, returning the
// same (matched, reason, mixedVintageDrop) triple as the historical
// k8sSelectMatch. This is the single tri-state decision tree; see the
// k8sSelectMatch doc comment above for the full selector-present/empty/absent
// and mixed-vintage semantics. Behavior is byte-for-byte identical to the
// pre-#5363 k8sSelectMatch body: the only change is that the workload's
// pod-template labels come from the once-parsed t.ParsedPodTemplateLabels
// instead of being re-parsed from the raw string on every call.
func (t K8sWorkloadMatchTarget) Match(service K8sSelectMatchInput) (matched bool, reason string, mixedVintageDrop bool) {
	if !strings.EqualFold(t.Workload.Kind, "Deployment") {
		return false, "", false
	}
	if !strings.EqualFold(service.Namespace, t.Workload.Namespace) {
		return false, "", false
	}

	if !service.SelectorPresent {
		if service.Name != "" && service.Name == t.Workload.Name {
			return true, K8sSelectReasonNameNamespace, false
		}
		return false, "", false
	}

	if service.Selector == "" {
		return false, "", false
	}

	if !t.Workload.PodTemplateLabelsPresent {
		return false, "", true
	}

	if K8sSelectorSubsetOfParsed(service.Selector, t.ParsedPodTemplateLabels) {
		return true, K8sSelectReasonSelectorMatch, false
	}
	return false, "", false
}

// K8sSelectorSubsetOf reports whether every key=value pair in selector
// (Eshu's sorted "k=v,k=v" encoding) is present with an equal value in
// labels. An empty selector is never a subset of anything -- callers must
// gate on a non-empty, known selector before calling this (see
// k8sSelectMatch); this guard exists so the emptiness rule holds even if a
// future caller forgets.
func K8sSelectorSubsetOf(selector, labels string) bool {
	if selector == "" {
		return false
	}
	return K8sSelectorSubsetOfParsed(selector, parseK8sLabelPairs(labels))
}

// K8sSelectorSubsetOfParsed is k8sSelectorSubsetOf with the label side already
// parsed, so a directed scan of many selectors against one prepared workload
// (K8sWorkloadMatchTarget.Match) parses the workload label map once. An empty
// selector is never a subset of anything (the emptiness rule, guarded here as
// well as by the caller). labelPairs may be nil (a workload whose
// pod_template_labels key is present but empty): a non-empty selector then has
// no matching pair and is correctly not a subset.
func K8sSelectorSubsetOfParsed(selector string, labelPairs map[string]string) bool {
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

// K8sSelectMatchInputFromEntity adapts an EntityContent row (the shape used
// by content_relationships.go) into K8sSelectMatchInput.
func K8sSelectMatchInputFromEntity(entity EntityContent) K8sSelectMatchInput {
	kind, _ := entity.Metadata["kind"].(string)
	selector, selectorPresent := entity.Metadata["selector"].(string)
	podTemplateLabels, podTemplateLabelsPresent := entity.Metadata["pod_template_labels"].(string)
	return K8sSelectMatchInput{
		Kind:                     kind,
		Name:                     entity.EntityName,
		Namespace:                K8sNamespace(entity.Metadata),
		Selector:                 selector,
		SelectorPresent:          selectorPresent,
		PodTemplateLabels:        podTemplateLabels,
		PodTemplateLabelsPresent: podTemplateLabelsPresent,
	}
}

// K8sSelectMatchInputFromCandidate adapts a K8sSelectCandidate into the
// shared K8sSelectMatchInput. The mapping is 1:1 with
// K8sSelectMatchInputFromEntity for the same source row, so a directed match
// over candidates produces byte-for-byte the same verdict the entity-context
// path would produce over the equivalent EntityContent. It lives here rather
// than in package query because the impact handler family (#6060 lane B2)
// matches candidates without importing the root package.
func K8sSelectMatchInputFromCandidate(c K8sSelectCandidate) K8sSelectMatchInput {
	return K8sSelectMatchInput{
		Kind:                     c.Kind,
		Name:                     c.EntityName,
		Namespace:                c.Namespace,
		Selector:                 c.Selector,
		SelectorPresent:          c.SelectorPresent,
		PodTemplateLabels:        c.PodTemplateLabels,
		PodTemplateLabelsPresent: c.PodTemplateLabelsPresent,
	}
}

// LogK8sSelectMixedVintageDrop logs a mixed-vintage matcher drop as a Debug
// diagnostic: the candidate workload predates pod_template_labels capture, so
// the drop self-heals on re-ingest. It lives here rather than in package
// query because the impact handler family (#6060 lane B2) logs these drops
// without importing the root package.
func LogK8sSelectMixedVintageDrop(ctx context.Context, logger *slog.Logger, serviceEntityID, workloadEntityID string) {
	if logger == nil {
		return
	}
	logger.DebugContext(
		ctx, "k8s SELECTS mixed-vintage drop: candidate workload predates pod_template_labels capture",
		"service_entity_id", serviceEntityID,
		"workload_entity_id", workloadEntityID,
	)
}

// K8sSelectMatchInputFromRow adapts a flattened map[string]any row (the
// shape used by impact_trace_deployment_k8s.go and its resource builders)
// into K8sSelectMatchInput. Presence of the "selector"/"pod_template_labels"
// keys in the row carries the same tri-state meaning as the metadata map
// keys on EntityContent -- callers that build these rows must omit the key
// entirely rather than set it to a zero value when the source data lacks it.
func K8sSelectMatchInputFromRow(row map[string]any) K8sSelectMatchInput {
	selector, selectorPresent := row["selector"].(string)
	podTemplateLabels, podTemplateLabelsPresent := row["pod_template_labels"].(string)
	return K8sSelectMatchInput{
		Kind:                     SafeStr(row, "kind"),
		Name:                     SafeStr(row, "entity_name"),
		Namespace:                SafeStr(row, "namespace"),
		Selector:                 selector,
		SelectorPresent:          selectorPresent,
		PodTemplateLabels:        podTemplateLabels,
		PodTemplateLabelsPresent: podTemplateLabelsPresent,
	}
}
