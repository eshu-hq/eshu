// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "sort"

// classifyObservabilityCoverage turns the bounded coverage index into one
// decision per observability coverage candidate plus one gap finding per
// uncovered monitored target. The invariant: a coverage edge is canonical truth
// (exact/derived, not provenance-only) only when an observability object
// resolves to a target by a stable identity; name coincidence and
// metric-name-only signals stay provenance and never fabricate a covered edge.
func classifyObservabilityCoverage(index observabilityCoverageIndex) []ObservabilityCoverageCorrelationDecision {
	var decisions []ObservabilityCoverageCorrelationDecision
	coveredTargets := make(map[string]struct{})

	for _, ref := range index.objectOrder {
		object := index.objectsByRef[ref]
		objectDecisions, covered := classifyObservabilityObject(object, index)
		decisions = append(decisions, objectDecisions...)
		for uid := range covered {
			coveredTargets[uid] = struct{}{}
		}
	}

	decisions = append(decisions, observabilityGapDecisions(index, coveredTargets)...)
	sortObservabilityCoverageDecisions(decisions)
	return decisions
}

// classifyObservabilityObject classifies one observability object into coverage
// decisions and returns the set of target uids it covers (for gap accounting).
func classifyObservabilityObject(
	object observabilityObject,
	index observabilityCoverageIndex,
) ([]ObservabilityCoverageCorrelationDecision, map[string]struct{}) {
	covered := make(map[string]struct{})
	rels := observabilityCoverageRelationships(object, index)
	if len(rels) == 0 {
		// An observability object with no coverage-bearing relationship (a
		// metadata-only dashboard, a bare log group) references no resource, so it
		// cannot prove coverage. It is suppressed as rejected, never covered.
		return []ObservabilityCoverageCorrelationDecision{
			rejectedObjectDecision(object, "observability object references no monitored resource"),
		}, covered
	}

	var decisions []ObservabilityCoverageCorrelationDecision
	for _, rel := range rels {
		decision, coversUID := classifyCoverageRelationship(object, rel, index)
		decisions = append(decisions, decision)
		if coversUID != "" {
			covered[coversUID] = struct{}{}
		}
	}
	return decisions, covered
}

// observabilityCoverageRelationships returns the resource-bearing coverage
// relationships for an object (alarm→metric dimensions, X-Ray rule→service). The
// paging fan-out relationship is intentionally excluded from coverage edges here
// because it targets an SNS topic, not a monitored resource.
func observabilityCoverageRelationships(
	object observabilityObject,
	index observabilityCoverageIndex,
) []coverageRelationship {
	var out []coverageRelationship
	for _, rel := range index.relsBySource[object.ref] {
		switch rel.relationshipType {
		case relAlarmObservesMetric, relXRayMatchesService:
			out = append(out, rel)
		}
	}
	return out
}

// classifyCoverageRelationship maps one coverage relationship to its outcome and
// returns the covered target uid when the outcome is exact/derived.
func classifyCoverageRelationship(
	object observabilityObject,
	rel coverageRelationship,
	index observabilityCoverageIndex,
) (ObservabilityCoverageCorrelationDecision, string) {
	base := baseCoverageDecision(object)
	base.EvidenceFactIDs = compactStringSlice(object.factID, rel.factID)

	// X-Ray service coverage resolves by service name, not a CloudResource uid;
	// it is real coverage but inferred, so it stays derived/provenance-only until
	// a canonical service-identity anchor exists.
	if rel.relationshipType == relXRayMatchesService {
		if rel.serviceRef == "" {
			return rejectedDecision(base, "x-ray sampling rule names no concrete service"), ""
		}
		base.TargetServiceRef = rel.serviceRef
		base.Outcome = ObservabilityCoverageDerived
		base.CoverageStatus = "covered"
		base.ProvenanceOnly = true
		base.ResolutionMode = "service_name"
		base.Reason = "x-ray sampling rule matches service by name"
		return base, ""
	}

	if len(rel.targetKeys) == 0 {
		return rejectedDecision(base, "alarm has no resolvable resource dimension (metric-name-only signal)"), ""
	}
	active, tombstoned := index.targets.resolve(rel.targetKeys)
	switch len(active) {
	case 0:
		if len(tombstoned) > 0 {
			base.Outcome = ObservabilityCoverageStale
			base.CoverageStatus = "stale"
			base.ProvenanceOnly = true
			base.CandidateTargetUIDs = sortedUIDs(tombstoned)
			base.Reason = "observability object resolved only to a tombstoned resource"
			return base, ""
		}
		base.Outcome = ObservabilityCoverageUnresolved
		base.CoverageStatus = "gap"
		base.ProvenanceOnly = true
		base.Reason = "observability object target is not a scanned resource in this generation"
		return base, ""
	case 1:
		uid := sortedUIDs(active)[0]
		base.TargetUID = uid
		base.Outcome = ObservabilityCoverageExact
		base.CoverageStatus = "covered"
		base.ProvenanceOnly = false
		base.ResolutionMode = active[uid].resolutionMode
		base.Reason = "alarm dimension resolves to a scanned resource identity"
		return base, uid
	default:
		base.Outcome = ObservabilityCoverageAmbiguous
		base.CoverageStatus = "ambiguous"
		base.ProvenanceOnly = true
		base.CandidateTargetUIDs = sortedUIDs(active)
		base.Reason = "alarm dimension matches multiple scanned resource identities"
		return base, ""
	}
}

// observabilityGapDecisions emits a gap finding for every monitored target
// resource that has no resolving observability coverage, bounded to resource
// classes that have at least one covered peer in scope (the memo Q2
// evidence-bounded default) so the output is not "everything is a gap." Gaps are
// keyed on the target and are provenance-only.
func observabilityGapDecisions(
	index observabilityCoverageIndex,
	coveredTargets map[string]struct{},
) []ObservabilityCoverageCorrelationDecision {
	coveredTypes := make(map[string]struct{})
	for uid := range coveredTargets {
		if resource, ok := index.targetByUID(uid); ok {
			coveredTypes[resource.resourceType] = struct{}{}
		}
	}

	seen := make(map[string]struct{})
	var decisions []ObservabilityCoverageCorrelationDecision
	for _, bucket := range index.targets.byKey {
		for uid, resource := range bucket {
			if resource.tombstone {
				continue
			}
			if _, isCovered := coveredTargets[uid]; isCovered {
				continue
			}
			if _, hasCoveredPeer := coveredTypes[resource.resourceType]; !hasCoveredPeer {
				continue
			}
			if _, dup := seen[uid]; dup {
				continue
			}
			seen[uid] = struct{}{}
			decisions = append(decisions, ObservabilityCoverageCorrelationDecision{
				Provider:       "aws",
				CoverageSignal: coverageSignalAlarm,
				TargetUID:      uid,
				Outcome:        ObservabilityCoverageUnresolved,
				CoverageStatus: "gap",
				ProvenanceOnly: true,
				SourceClass:    "observed",
				SourceClasses:  []string{"observed"},
				SourceKind:     "aws",
				SourceKinds:    []string{"aws"},
				ResourceClass:  resource.resourceType,
				FreshnessState: "current",
				Reason:         "monitored resource has no resolving observability coverage",
			})
		}
	}
	return decisions
}

func (index observabilityCoverageIndex) targetByUID(uid string) (targetResource, bool) {
	for _, bucket := range index.targets.byKey {
		if resource, ok := bucket[uid]; ok {
			return resource, true
		}
	}
	return targetResource{}, false
}

func baseCoverageDecision(object observabilityObject) ObservabilityCoverageCorrelationDecision {
	return ObservabilityCoverageCorrelationDecision{
		Provider:               "aws",
		CoverageSignal:         object.signal,
		ObservabilityObjectRef: object.ref,
		ObservabilityUID:       object.uid,
		ProvenanceOnly:         true,
		SourceClass:            "observed",
		SourceClasses:          []string{"observed"},
		SourceKind:             "aws",
		SourceKinds:            []string{"aws"},
		ResourceClass:          object.resourceType,
		FreshnessState:         "current",
		EvidenceFactIDs:        compactStringSlice(object.factID),
	}
}

func rejectedObjectDecision(object observabilityObject, reason string) ObservabilityCoverageCorrelationDecision {
	return rejectedDecision(baseCoverageDecision(object), reason)
}

func rejectedDecision(base ObservabilityCoverageCorrelationDecision, reason string) ObservabilityCoverageCorrelationDecision {
	base.Outcome = ObservabilityCoverageRejected
	base.CoverageStatus = "rejected"
	base.ProvenanceOnly = true
	base.Reason = reason
	return base
}

func sortedUIDs(resources map[string]targetResource) []string {
	uids := make([]string, 0, len(resources))
	for uid := range resources {
		uids = append(uids, uid)
	}
	sort.Strings(uids)
	return uids
}

// sortObservabilityCoverageDecisions orders decisions deterministically so the
// batched fact write is stable across retries and reprojections.
func sortObservabilityCoverageDecisions(decisions []ObservabilityCoverageCorrelationDecision) {
	sort.SliceStable(decisions, func(i, j int) bool {
		left, right := decisions[i], decisions[j]
		if left.CoverageSignal != right.CoverageSignal {
			return left.CoverageSignal < right.CoverageSignal
		}
		if left.ObservabilityObjectRef != right.ObservabilityObjectRef {
			return left.ObservabilityObjectRef < right.ObservabilityObjectRef
		}
		if left.TargetUID != right.TargetUID {
			return left.TargetUID < right.TargetUID
		}
		return left.TargetServiceRef < right.TargetServiceRef
	})
}
