// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	incidentRoutingSourceClassApplied = "applied"
	incidentRoutingSourceKindTFState  = "terraform_state"
)

var pagerDutyAppliedResourceClasses = map[string]string{
	"pagerduty_escalation_policy":    "escalation_policy",
	"pagerduty_event_orchestration":  "event_orchestration",
	"pagerduty_service":              "service",
	"pagerduty_service_integration":  "service_integration",
	"pagerduty_team":                 "team",
	"pagerduty_webhook_subscription": "webhook_subscription",
	"pagerduty_business_service":     "business_service",
	"pagerduty_service_event_rule":   "service_event_rule",
	"pagerduty_service_dependency":   "service_dependency",
	"pagerduty_slack_connection":     "slack_connection",
}

var alertRouteResourceTypes = map[string]string{
	"aws_cloudwatch_event_rule":   "event_rule",
	"aws_cloudwatch_event_target": "event_target",
	"aws_cloudwatch_metric_alarm": "cloudwatch_alarm",
	"aws_dynamodb_table":          "dynamodb_config_table",
	"aws_iam_policy":              "iam_policy",
	"aws_lambda_function":         "lambda_function",
	"aws_sns_topic":               "sns_topic",
	"aws_sns_topic_subscription":  "sns_subscription",
	"aws_ssm_parameter":           "ssm_parameter",
}

func (p *stateParser) emitAppliedIncidentRoutingEvidence(
	resource resourceContext,
	address string,
	attributes []attributeValue,
) error {
	resourceType := strings.TrimSpace(resource.Type)
	attributeByKey := attributeMap(attributes)
	if _, ok := pagerDutyAppliedResourceClasses[resourceType]; ok {
		return p.emitAppliedPagerDutyResource(resource, address, attributeByKey)
	}
	if strings.HasPrefix(resourceType, "pagerduty_") {
		return p.emitIncidentRoutingCoverageWarning(resource, address, "unsupported_pagerduty_resource")
	}
	if routeType, ok := alertRouteResourceTypes[resourceType]; ok && isPagerDutyAlertRouteCandidate(resource, address, attributeByKey) {
		return p.emitAppliedAlertRoute(resource, address, routeType, attributeByKey)
	}
	return nil
}

func (p *stateParser) emitAppliedPagerDutyResource(
	resource resourceContext,
	address string,
	attributes map[string]attributeValue,
) error {
	resourceType := strings.TrimSpace(resource.Type)
	payload := p.incidentRoutingBasePayload(resource, address)
	payload["resource_class"] = pagerDutyAppliedResourceClasses[resourceType]

	if id, ok := scalarString(attributes["id"]); ok {
		payload["provider_object_id"] = id
	}
	if name, ok := scalarString(attributes["name"]); ok {
		payload["name_fingerprint"] = incidentRoutingFingerprint(name)
	}
	if escalationPolicy, ok := scalarString(attributes["escalation_policy"]); ok {
		payload["escalation_policy_reference"] = escalationPolicy
	}
	if service, ok := scalarString(attributes["service"]); ok {
		payload["service_reference"] = service
	}
	if integrationType, ok := firstScalarString(attributes, "type", "integration_type"); ok {
		payload["integration_type"] = integrationType
	}
	if alertCreation, ok := scalarString(attributes["alert_creation"]); ok {
		payload["alert_creation"] = alertCreation
	}
	if deliveryMethod, ok := scalarString(attributes["delivery_method"]); ok {
		payload["delivery_method"] = deliveryMethod
	}
	if objectID, ok := scalarString(attributes["webhook_object_id"]); ok {
		payload["webhook_object_reference"] = objectID
	}
	if objectType, ok := scalarString(attributes["object_type"]); ok {
		payload["webhook_object_type"] = objectType
	}
	recordRedactedPresence(
		payload,
		attributes,
		"config",
		"email",
		"html_url",
		"integration_key",
		"private_url",
		"routing_key",
		"secret",
		"url",
		"webhook_secret",
	)

	stableKey := "applied_pagerduty_resource:" + address
	return p.emitBodyFact(p.incidentRoutingEnvelope(
		facts.IncidentRoutingAppliedPagerDutyResourceFactKind,
		stableKey,
		payload,
		address,
	))
}

func (p *stateParser) emitAppliedAlertRoute(
	resource resourceContext,
	address string,
	routeType string,
	attributes map[string]attributeValue,
) error {
	payload := p.incidentRoutingBasePayload(resource, address)
	payload["route_type"] = routeType
	if arn, ok := scalarString(attributes["arn"]); ok && strings.HasPrefix(arn, "arn:") {
		payload["aws_arn"] = arn
	}
	for _, key := range []string{"name", "function_name", "target_id", "rule"} {
		if value, ok := scalarString(attributes[key]); ok {
			payload[key+"_fingerprint"] = incidentRoutingFingerprint(value)
		}
	}
	if endpoint, ok := scalarString(attributes["endpoint"]); ok {
		payload["endpoint_redacted"] = true
		payload["target_reference_kind"] = "pagerduty_endpoint_redacted"
		payload["target_reference_fingerprint"] = incidentRoutingFingerprint(endpoint)
	}
	if value, ok := scalarString(attributes["value"]); ok {
		payload["value_redacted"] = true
		if containsPagerDuty(value) {
			payload["target_reference_kind"] = "pagerduty_reference_redacted"
			payload["target_reference_fingerprint"] = incidentRoutingFingerprint(value)
		}
	}
	if _, ok := attributes["policy"]; ok {
		payload["policy_redacted"] = true
	}
	setRoutingRedactionState(payload)

	stableKey := "applied_alert_route:" + address
	return p.emitBodyFact(p.incidentRoutingEnvelope(
		facts.IncidentRoutingAppliedAlertRouteFactKind,
		stableKey,
		payload,
		address,
	))
}

func (p *stateParser) emitIncidentRoutingCoverageWarning(resource resourceContext, address string, reason string) error {
	payload := p.incidentRoutingBasePayload(resource, address)
	payload["outcome"] = "unsupported"
	payload["reason"] = reason
	payload["redaction_state"] = "none"
	stableKey := "coverage_warning:" + reason + ":" + address
	return p.emitBodyFact(p.incidentRoutingEnvelope(
		facts.IncidentRoutingCoverageWarningFactKind,
		stableKey,
		payload,
		address,
	))
}

func (p *stateParser) incidentRoutingBasePayload(resource resourceContext, address string) map[string]any {
	payload := map[string]any{
		"source_class":            incidentRoutingSourceClassApplied,
		"source_kind":             incidentRoutingSourceKindTFState,
		"outcome":                 "applied",
		"terraform_state_address": address,
		"resource_type":           strings.TrimSpace(resource.Type),
		"resource_name":           strings.TrimSpace(resource.Name),
		"module_address":          strings.TrimSpace(resource.Module),
		"provider_address":        strings.TrimSpace(resource.Provider),
		"scope_id":                p.options.Scope.ScopeID,
		"state_generation_id":     p.options.Generation.GenerationID,
		"state_lineage":           p.snapshot.Lineage,
		"state_serial":            p.snapshot.Serial,
		"backend_kind":            string(p.options.Source.BackendKind),
		"locator_hash":            locatorHash(p.options.Source),
		"declared_match_state":    "not_compared",
	}
	payload["redaction_state"] = "none"
	return payload
}

func (p *stateParser) incidentRoutingEnvelope(kind string, stableKey string, payload map[string]any, sourceRecordID string) facts.Envelope {
	version, _ := facts.IncidentRoutingSchemaVersion(kind)
	key := kind + ":" + stableKey
	return facts.Envelope{
		FactID: facts.StableID("IncidentRoutingFact", map[string]any{
			"fact_kind":     kind,
			"stable_key":    key,
			"scope_id":      p.options.Scope.ScopeID,
			"generation_id": p.options.Generation.GenerationID,
		}),
		ScopeID:          p.options.Scope.ScopeID,
		GenerationID:     p.options.Generation.GenerationID,
		FactKind:         kind,
		StableFactKey:    key,
		SchemaVersion:    version,
		CollectorKind:    string(scope.CollectorTerraformState),
		FencingToken:     p.options.FencingToken,
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       p.options.ObservedAt,
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   string(scope.CollectorTerraformState),
			ScopeID:        p.options.Scope.ScopeID,
			GenerationID:   p.options.Generation.GenerationID,
			FactKey:        key,
			SourceURI:      sourceURI(p.options.Source),
			SourceRecordID: sourceRecordID,
		},
	}
}

func attributeMap(attributes []attributeValue) map[string]attributeValue {
	byKey := make(map[string]attributeValue, len(attributes))
	for _, attribute := range attributes {
		byKey[attribute.Key] = attribute
	}
	return byKey
}

func firstScalarString(attributes map[string]attributeValue, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := scalarString(attributes[key]); ok {
			return value, true
		}
	}
	return "", false
}

func scalarString(attribute attributeValue) (string, bool) {
	if !attribute.Scalar {
		return "", false
	}
	switch typed := attribute.Value.(type) {
	case string:
		typed = strings.TrimSpace(typed)
		if typed == "" {
			return "", false
		}
		return typed, true
	case json.Number:
		return typed.String(), typed.String() != ""
	case bool:
		if typed {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}

func recordRedactedPresence(payload map[string]any, attributes map[string]attributeValue, keys ...string) {
	redacted := make([]string, 0, len(keys))
	for _, key := range keys {
		if _, ok := attributes[key]; ok {
			redacted = append(redacted, key)
			payload[key+"_redacted"] = true
		}
	}
	if len(redacted) > 0 {
		payload["redacted_attributes"] = strings.Join(redacted, ",")
		payload["redaction_state"] = "redacted"
	}
}

func setRoutingRedactionState(payload map[string]any) {
	for _, key := range []string{"endpoint_redacted", "value_redacted", "policy_redacted"} {
		if payload[key] == true {
			payload["redaction_state"] = "redacted"
			return
		}
	}
	payload["redaction_state"] = "none"
}

func isPagerDutyAlertRouteCandidate(resource resourceContext, address string, attributes map[string]attributeValue) bool {
	for _, value := range []string{address, resource.Module, resource.Name} {
		if containsPagerDuty(value) {
			return true
		}
	}
	for _, key := range []string{"arn", "name", "function_name", "target_id", "rule", "endpoint", "value"} {
		if value, ok := scalarString(attributes[key]); ok && containsPagerDuty(value) {
			return true
		}
	}
	return false
}

func containsPagerDuty(value string) bool {
	return strings.Contains(strings.ToLower(value), "pagerduty")
}

func incidentRoutingFingerprint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(value)))
	return hex.EncodeToString(sum[:])[:16]
}
