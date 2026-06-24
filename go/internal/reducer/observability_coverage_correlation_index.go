// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Coverage signal classes (issue #391). They map the AWS-native observability
// objects to the issue's dashboard/monitor/scrape/rule/pipeline/alert vocabulary
// and form a bounded, stable metric dimension.
const (
	coverageSignalAlarm          = "alarm"
	coverageSignalCompositeAlarm = "composite_alarm"
	coverageSignalDashboard      = "dashboard"
	coverageSignalLogGroup       = "log_group"
	coverageSignalTraceSampling  = "trace_sampling"
)

// Observability object resource types on main that PR1 correlates. Each maps to
// one coverage signal class.
var observabilityResourceSignals = map[string]string{
	"aws_cloudwatch_alarm":           coverageSignalAlarm,
	"aws_cloudwatch_composite_alarm": coverageSignalCompositeAlarm,
	"aws_cloudwatch_dashboard":       coverageSignalDashboard,
	"aws_cloudwatch_logs_log_group":  coverageSignalLogGroup,
	"aws_xray_sampling_rule":         coverageSignalTraceSampling,
	"aws_xray_group":                 coverageSignalTraceSampling,
}

// Observability relationship types that carry a coverage target (alarm→resource
// via metric dimension, X-Ray rule→service by name).
const (
	relAlarmObservesMetric = "cloudwatch_alarm_observes_metric"
	relXRayMatchesService  = "xray_sampling_rule_matches_service"
)

// Resolution modes for observability coverage target matches (issue #391). They
// mirror the AWS relationship edge join_mode enum (issue #805) so the durable
// fact records which identity path matched: a target's ARN, its bare resource
// id, or one of its published correlation anchors.
const (
	coverageResolutionARN               = joinModeARN
	coverageResolutionBareID            = joinModeBareID
	coverageResolutionCorrelationAnchor = joinModeCorrelationAnchor
)

// observabilityTargetIndex resolves a monitored resource identity to the uid(s)
// of materialized CloudResource nodes. It is built once per scope generation
// from the non-observability aws_resource facts so target resolution is O(1) per
// edge — no per-edge graph round trip, no N+1 (the #805 §5.1 bounded join).
//
// A key can resolve to multiple uids (a dimension value that is non-unique
// across regions/accounts in scope); those keys yield an ambiguous outcome and
// never an exact pick. Tombstoned-only matches surface as stale.
type observabilityTargetIndex struct {
	byKey map[string]map[string]targetResource // identity key -> uid -> resource
}

// targetResource is one monitored CloudResource candidate. resolutionMode records
// which identity path (ARN / bare id / correlation anchor) registered this entry
// so an exact pick can report the join mode that actually matched rather than a
// hardcoded value.
type targetResource struct {
	uid            string
	resourceType   string
	tombstone      bool
	resolutionMode string
}

// observabilityObject is one observability source object (alarm, dashboard, log
// group, X-Ray rule) discovered as an aws_resource fact.
type observabilityObject struct {
	ref          string
	uid          string
	signal       string
	resourceType string
	factID       string
}

// coverageRelationship is one observability aws_relationship fact, pre-extracted
// into the fields the classifier needs.
type coverageRelationship struct {
	factID           string
	relationshipType string
	sourceRef        string
	targetKeys       []string
	serviceRef       string
}

// observabilityCoverageIndex is the bounded in-memory model the classifier reads.
type observabilityCoverageIndex struct {
	targets      observabilityTargetIndex
	objectsByRef map[string]observabilityObject
	objectOrder  []string
	relsBySource map[string][]coverageRelationship
}

// buildObservabilityCoverageIndex partitions the scope generation's facts into
// monitored-target resources, observability objects, and coverage relationships.
func buildObservabilityCoverageIndex(envelopes []facts.Envelope) observabilityCoverageIndex {
	index := observabilityCoverageIndex{
		targets:      observabilityTargetIndex{byKey: make(map[string]map[string]targetResource)},
		objectsByRef: make(map[string]observabilityObject),
		relsBySource: make(map[string][]coverageRelationship),
	}
	for _, env := range envelopes {
		switch env.FactKind {
		case facts.AWSResourceFactKind:
			index.ingestResource(env)
		case facts.AWSRelationshipFactKind:
			index.ingestRelationship(env)
		}
	}
	sort.Strings(index.objectOrder)
	return index
}

func (index *observabilityCoverageIndex) ingestResource(env facts.Envelope) {
	resourceType := payloadString(env.Payload, "resource_type")
	if resourceType == "" {
		return
	}
	if signal, ok := observabilityResourceSignals[resourceType]; ok {
		// A tombstoned observability object (a deleted alarm/dashboard/rule) is no
		// longer live coverage. Ingesting it would let a stale relationship fact
		// classify a monitored target as exact/covered and overstate current
		// coverage, so tombstoned observability objects never enter the index. The
		// uncovered target then surfaces correctly as a gap.
		if env.IsTombstone {
			return
		}
		index.ingestObservabilityObject(env, resourceType, signal)
		return
	}
	index.ingestTargetResource(env, resourceType)
}

func (index *observabilityCoverageIndex) ingestObservabilityObject(env facts.Envelope, resourceType, signal string) {
	arn := payloadString(env.Payload, "arn")
	resourceID := payloadString(env.Payload, "resource_id")
	ref := firstNonBlank(arn, resourceID)
	if ref == "" {
		return
	}
	uid := cloudResourceUID(
		payloadString(env.Payload, "account_id"),
		payloadString(env.Payload, "region"),
		resourceType,
		firstNonBlank(resourceID, arn),
	)
	if _, exists := index.objectsByRef[ref]; !exists {
		index.objectOrder = append(index.objectOrder, ref)
	}
	index.objectsByRef[ref] = observabilityObject{
		ref:          ref,
		uid:          uid,
		signal:       signal,
		resourceType: resourceType,
		factID:       env.FactID,
	}
}

func (index *observabilityCoverageIndex) ingestTargetResource(env facts.Envelope, resourceType string) {
	accountID := payloadString(env.Payload, "account_id")
	region := payloadString(env.Payload, "region")
	resourceID := payloadString(env.Payload, "resource_id")
	arn := payloadString(env.Payload, "arn")
	if resourceID == "" {
		resourceID = arn
	}
	if resourceID == "" {
		return
	}
	uid := cloudResourceUID(accountID, region, resourceType, resourceID)
	for _, ident := range targetIdentityKeys(arn, resourceID, env.Payload) {
		index.targets.add(ident.key, targetResource{
			uid:            uid,
			resourceType:   resourceType,
			tombstone:      env.IsTombstone,
			resolutionMode: ident.mode,
		})
	}
}

// targetIdentity is one identity key for a monitored resource paired with the
// resolution mode it represents (ARN, bare id, or correlation anchor).
type targetIdentity struct {
	key  string
	mode string
}

// targetIdentityKeys returns the identity keys an observability relationship can
// use to resolve this resource — its ARN, its bare resource id, and its
// published correlation anchors — each tagged with the resolution mode it
// represents. Keys are the same precise identities the #805 join index uses so
// resolution stays exact and the matched join mode is preserved on the fact.
func targetIdentityKeys(arn, resourceID string, payload map[string]any) []targetIdentity {
	var idents []targetIdentity
	if trimmed := strings.TrimSpace(arn); trimmed != "" {
		idents = append(idents, targetIdentity{key: trimmed, mode: coverageResolutionARN})
	}
	if trimmed := strings.TrimSpace(resourceID); trimmed != "" {
		idents = append(idents, targetIdentity{key: trimmed, mode: coverageResolutionBareID})
	}
	for _, anchor := range payloadStrings(payload, "", "correlation_anchors") {
		if trimmed := strings.TrimSpace(anchor); trimmed != "" {
			idents = append(idents, targetIdentity{key: trimmed, mode: coverageResolutionCorrelationAnchor})
		}
	}
	return idents
}

// add registers one identity key for a resource. When the same uid is reachable
// through multiple key classes, the more specific mode wins (ARN over bare id
// over anchor) so an exact match reports the strongest identity that matched.
func (i observabilityTargetIndex) add(key string, resource targetResource) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	bucket, ok := i.byKey[key]
	if !ok {
		bucket = make(map[string]targetResource)
		i.byKey[key] = bucket
	}
	if existing, dup := bucket[resource.uid]; dup &&
		coverageResolutionRank(existing.resolutionMode) <= coverageResolutionRank(resource.resolutionMode) {
		return
	}
	bucket[resource.uid] = resource
}

// coverageResolutionRank orders resolution modes from most to least specific so
// add can keep the strongest mode when one key string is registered for a uid
// under more than one class. Lower rank is more specific.
func coverageResolutionRank(mode string) int {
	switch mode {
	case coverageResolutionARN:
		return 0
	case coverageResolutionBareID:
		return 1
	case coverageResolutionCorrelationAnchor:
		return 2
	default:
		return 3
	}
}

func (index *observabilityCoverageIndex) ingestRelationship(env facts.Envelope) {
	relationshipType := payloadString(env.Payload, "relationship_type")
	source := firstNonBlank(payloadString(env.Payload, "source_arn"), payloadString(env.Payload, "source_resource_id"))
	if source == "" {
		return
	}
	rel := coverageRelationship{
		factID:           env.FactID,
		relationshipType: relationshipType,
		sourceRef:        source,
	}
	switch relationshipType {
	case relAlarmObservesMetric:
		rel.targetKeys = alarmDimensionTargetKeys(env.Payload)
	case relXRayMatchesService:
		rel.serviceRef = relationshipServiceRef(env.Payload)
	default:
		// Only resource-bearing coverage relationships are indexed in PR1. The
		// alarm→SNS paging fan-out targets an SNS topic, not a monitored resource,
		// so it carries no coverage edge and is skipped.
		return
	}
	index.relsBySource[source] = append(index.relsBySource[source], rel)
}

// alarmDimensionTargetKeys extracts the resource identity from a
// cloudwatch_alarm_observes_metric fact's redacted dimension summary. AWS system
// dimension values (InstanceId, FunctionName, DBInstanceIdentifier, …) are the
// bare resource id of the monitored CloudResource and are not redacted;
// customer-tag dimension values were redacted at the scanner and contribute
// nothing. An alarm whose dimensions resolve to nothing is a metric-name-only
// signal and is rejected by the classifier.
func alarmDimensionTargetKeys(payload map[string]any) []string {
	attributes, ok := payload["attributes"].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := attributes["dimensions"].([]any)
	if !ok {
		return nil
	}
	var keys []string
	for _, entry := range raw {
		dim, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		value := strings.TrimSpace(payloadString(dim, "value"))
		if value == "" {
			continue
		}
		keys = append(keys, value)
	}
	return keys
}

func relationshipServiceRef(payload map[string]any) string {
	attributes, ok := payload["attributes"].(map[string]any)
	if !ok {
		return ""
	}
	return payloadString(attributes, "service_name")
}

// resolve returns the active and tombstoned-only resource matches for a key set.
// A key that maps to multiple active uids is the ambiguity signal; the caller
// decides the outcome.
func (i observabilityTargetIndex) resolve(keys []string) (active map[string]targetResource, tombstoned map[string]targetResource) {
	active = make(map[string]targetResource)
	tombstoned = make(map[string]targetResource)
	for _, key := range keys {
		bucket, ok := i.byKey[strings.TrimSpace(key)]
		if !ok {
			continue
		}
		for uid, resource := range bucket {
			if resource.tombstone {
				if _, live := active[uid]; !live {
					tombstoned[uid] = resource
				}
				continue
			}
			active[uid] = resource
			delete(tombstoned, uid)
		}
	}
	return active, tombstoned
}
