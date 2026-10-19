// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import "fmt"

// DecodeRelationshipCloudWatchAlarmObservesMetricAttributes decodes the nested
// Attributes["attributes"] dimensions field off an already-decoded
// cloudwatch_alarm_observes_metric aws_relationship Relationship (see
// RelationshipCloudWatchAlarmObservesMetricAttributes).
func DecodeRelationshipCloudWatchAlarmObservesMetricAttributes(rel Relationship) (RelationshipCloudWatchAlarmObservesMetricAttributes, error) {
	nested := nestedAttributes(rel.Attributes)
	raw, ok := nested["dimensions"]
	if !ok || raw == nil {
		return RelationshipCloudWatchAlarmObservesMetricAttributes{}, nil
	}
	entries, ok := anyMapSlice(raw)
	if !ok {
		return RelationshipCloudWatchAlarmObservesMetricAttributes{}, newAttributeShapeError(
			"attributes.dimensions", fmt.Sprintf("want array, got %T", raw),
		)
	}
	dims := make([]MetricDimension, 0, len(entries))
	for i, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			return RelationshipCloudWatchAlarmObservesMetricAttributes{}, newAttributeShapeError(
				fmt.Sprintf("attributes.dimensions[%d]", i), fmt.Sprintf("want object, got %T", entry),
			)
		}
		value, err := attributeString(m, fmt.Sprintf("attributes.dimensions[%d].value", i), "value")
		if err != nil {
			return RelationshipCloudWatchAlarmObservesMetricAttributes{}, err
		}
		dims = append(dims, MetricDimension{Value: value})
	}
	return RelationshipCloudWatchAlarmObservesMetricAttributes{Dimensions: dims}, nil
}

// DecodeRelationshipXRaySamplingRuleMatchesServiceAttributes decodes the
// nested Attributes["attributes"] service_name field off an already-decoded
// xray_sampling_rule_matches_service aws_relationship Relationship (see
// RelationshipXRaySamplingRuleMatchesServiceAttributes).
func DecodeRelationshipXRaySamplingRuleMatchesServiceAttributes(rel Relationship) (RelationshipXRaySamplingRuleMatchesServiceAttributes, error) {
	nested := nestedAttributes(rel.Attributes)
	serviceName, err := attributeString(nested, "attributes.service_name", "service_name")
	if err != nil {
		return RelationshipXRaySamplingRuleMatchesServiceAttributes{}, err
	}
	return RelationshipXRaySamplingRuleMatchesServiceAttributes{ServiceName: serviceName}, nil
}
