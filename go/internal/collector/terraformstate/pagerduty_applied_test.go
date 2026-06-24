// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestParserEmitsAppliedPagerDutyIncidentRoutingFacts(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","resources":[
		{"mode":"managed","type":"pagerduty_service","name":"checkout","module":"module.pagerduty","provider":"provider[\"registry.terraform.io/PagerDuty/pagerduty\"]","instances":[{"attributes":{"id":"PDSVC1","name":"Checkout API","description":"Checkout owned PagerDuty service","escalation_policy":"PDEP1","alert_creation":"create_alerts_and_incidents"}}]},
		{"mode":"managed","type":"pagerduty_escalation_policy","name":"checkout","module":"module.pagerduty","provider":"provider[\"registry.terraform.io/PagerDuty/pagerduty\"]","instances":[{"attributes":{"id":"PDEP1","name":"Checkout Escalation"}}]},
		{"mode":"managed","type":"pagerduty_team","name":"checkout","module":"module.pagerduty","provider":"provider[\"registry.terraform.io/PagerDuty/pagerduty\"]","instances":[{"attributes":{"id":"PDTEAM1","name":"Checkout Team"}}]},
		{"mode":"managed","type":"pagerduty_service_integration","name":"events","module":"module.pagerduty","provider":"provider[\"registry.terraform.io/PagerDuty/pagerduty\"]","instances":[{"attributes":{"id":"PDINT1","name":"Events API","service":"PDSVC1","integration_key":"secret-routing-key","type":"events_api_v2"}}]},
		{"mode":"managed","type":"pagerduty_event_orchestration","name":"checkout","module":"module.pagerduty","provider":"provider[\"registry.terraform.io/PagerDuty/pagerduty\"]","instances":[{"attributes":{"id":"PDORCH1","name":"Checkout Orchestration"}}]},
		{"mode":"managed","type":"pagerduty_webhook_subscription","name":"checkout","module":"module.pagerduty","provider":"provider[\"registry.terraform.io/PagerDuty/pagerduty\"]","instances":[{"attributes":{"id":"PDWEB1","delivery_method":"http_delivery_method","type":"incident.triggered","webhook_object_id":"PDSVC1","object_type":"service"}}]},
		{"mode":"managed","type":"pagerduty_user","name":"owner","module":"module.pagerduty","provider":"provider[\"registry.terraform.io/PagerDuty/pagerduty\"]","instances":[{"attributes":{"id":"PDUSER1","email":"owner@example.com"}}]}
	]}`

	result := parseFixtureFacts(t, state)
	applied := factsByKind(result, facts.IncidentRoutingAppliedPagerDutyResourceFactKind)
	if got, want := len(applied), 6; got != want {
		t.Fatalf("applied PagerDuty fact count = %d, want %d: %#v", got, want, applied)
	}

	service := factByPayloadValue(t, applied, "terraform_state_address", "module.pagerduty.pagerduty_service.checkout")
	if got, want := service.Payload["source_class"], "applied"; got != want {
		t.Fatalf("source_class = %#v, want %#v", got, want)
	}
	if got, want := service.Payload["source_kind"], "terraform_state"; got != want {
		t.Fatalf("source_kind = %#v, want %#v", got, want)
	}
	if got, want := service.Payload["provider_object_id"], "PDSVC1"; got != want {
		t.Fatalf("provider_object_id = %#v, want %#v", got, want)
	}
	if got, want := service.Payload["module_address"], "module.pagerduty"; got != want {
		t.Fatalf("module_address = %#v, want %#v", got, want)
	}
	generationID, ok := service.Payload["state_generation_id"].(string)
	if !ok ||
		!strings.HasPrefix(generationID, "terraform_state:state_snapshot:s3:") ||
		!strings.Contains(generationID, ":lineage-123:serial:17") {
		t.Fatalf("state_generation_id = %#v, want Terraform-state snapshot generation", service.Payload["state_generation_id"])
	}
	if got, ok := service.Payload["name_fingerprint"].(string); !ok || strings.TrimSpace(got) == "" {
		t.Fatalf("name_fingerprint = %#v, want non-empty string", service.Payload["name_fingerprint"])
	}
	if _, exists := service.Payload["name"]; exists {
		t.Fatalf("service raw name leaked in payload: %#v", service.Payload)
	}

	integration := factByPayloadValue(t, applied, "resource_type", "pagerduty_service_integration")
	if got, want := integration.Payload["service_reference"], "PDSVC1"; got != want {
		t.Fatalf("service_reference = %#v, want %#v", got, want)
	}
	if _, exists := integration.Payload["integration_key"]; exists {
		t.Fatalf("integration_key leaked in payload: %#v", integration.Payload)
	}

	warnings := factsByKind(result, facts.IncidentRoutingCoverageWarningFactKind)
	warning := factByPayloadValue(t, warnings, "resource_type", "pagerduty_user")
	if got, want := warning.Payload["outcome"], "unsupported"; got != want {
		t.Fatalf("warning outcome = %#v, want %#v", got, want)
	}
	if got, want := warning.Payload["reason"], "unsupported_pagerduty_resource"; got != want {
		t.Fatalf("warning reason = %#v, want %#v", got, want)
	}
	assertNoRawSecret(t, result, "secret-routing-key")
	assertNoRawSecret(t, result, "owner@example.com")
	assertNoRawSecretInRefs(t, result, "secret-routing-key")
}

func TestParserEmitsAppliedPagerDutyAlertRouteFacts(t *testing.T) {
	t.Parallel()

	state := `{"serial":17,"lineage":"lineage-123","resources":[
		{"mode":"managed","type":"aws_sns_topic_subscription","name":"pagerduty","module":"module.alerts","provider":"provider[\"registry.terraform.io/hashicorp/aws\"]","instances":[{"attributes":{"arn":"arn:aws:sns:us-east-1:123456789012:pagerduty:sub1","protocol":"https","endpoint":"https://events.pagerduty.com/integration/secret-token/enqueue"}}]},
		{"mode":"managed","type":"aws_ssm_parameter","name":"pagerduty_endpoint","module":"module.alerts","provider":"provider[\"registry.terraform.io/hashicorp/aws\"]","instances":[{"attributes":{"arn":"arn:aws:ssm:us-east-1:123456789012:parameter/pagerduty/endpoint","name":"/pagerduty/endpoint","value":"https://events.pagerduty.com/integration/secret-token/enqueue"}}]},
		{"mode":"managed","type":"aws_lambda_function","name":"pagerduty_router","module":"module.alerts","provider":"provider[\"registry.terraform.io/hashicorp/aws\"]","instances":[{"attributes":{"arn":"arn:aws:lambda:us-east-1:123456789012:function:pagerduty-router","function_name":"pagerduty-router"}}]},
		{"mode":"managed","type":"aws_iam_policy","name":"pagerduty_router","module":"module.alerts","provider":"provider[\"registry.terraform.io/hashicorp/aws\"]","instances":[{"attributes":{"arn":"arn:aws:iam::123456789012:policy/pagerduty-router","name":"pagerduty-router-policy","policy":"{\"Statement\":[{\"Resource\":\"secret-token\"}]}"} }]},
		{"mode":"managed","type":"aws_dynamodb_table","name":"pagerduty_config","module":"module.alerts","provider":"provider[\"registry.terraform.io/hashicorp/aws\"]","instances":[{"attributes":{"arn":"arn:aws:dynamodb:us-east-1:123456789012:table/pagerduty-config","name":"pagerduty-config"}}]},
		{"mode":"managed","type":"aws_cloudwatch_event_target","name":"pagerduty","module":"module.alerts","provider":"provider[\"registry.terraform.io/hashicorp/aws\"]","instances":[{"attributes":{"arn":"arn:aws:lambda:us-east-1:123456789012:function:pagerduty-router","rule":"critical-alerts","target_id":"pagerduty-router"}}]}
	]}`

	result := parseFixtureFacts(t, state)
	routes := factsByKind(result, facts.IncidentRoutingAppliedAlertRouteFactKind)
	if got, want := len(routes), 6; got != want {
		t.Fatalf("applied alert-route fact count = %d, want %d: %#v", got, want, routes)
	}

	subscription := factByPayloadValue(t, routes, "resource_type", "aws_sns_topic_subscription")
	if got, want := subscription.Payload["route_type"], "sns_subscription"; got != want {
		t.Fatalf("route_type = %#v, want %#v", got, want)
	}
	if got, want := subscription.Payload["target_reference_kind"], "pagerduty_endpoint_redacted"; got != want {
		t.Fatalf("target_reference_kind = %#v, want %#v", got, want)
	}
	if _, exists := subscription.Payload["endpoint"]; exists {
		t.Fatalf("endpoint leaked in payload: %#v", subscription.Payload)
	}

	parameter := factByPayloadValue(t, routes, "resource_type", "aws_ssm_parameter")
	if got, want := parameter.Payload["value_redacted"], true; got != want {
		t.Fatalf("value_redacted = %#v, want %#v", got, want)
	}
	if _, exists := parameter.Payload["value"]; exists {
		t.Fatalf("ssm value leaked in payload: %#v", parameter.Payload)
	}

	policy := factByPayloadValue(t, routes, "resource_type", "aws_iam_policy")
	if got, want := policy.Payload["policy_redacted"], true; got != want {
		t.Fatalf("policy_redacted = %#v, want %#v", got, want)
	}
	if _, exists := policy.Payload["policy"]; exists {
		t.Fatalf("policy document leaked in payload: %#v", policy.Payload)
	}

	assertNoRawSecret(t, result, "secret-token")
	assertNoRawSecretInRefs(t, result, "secret-token")
}

func TestParserRedactsAppliedRoutingSensitiveAttributesUnderKnownSchema(t *testing.T) {
	t.Parallel()

	options := parseFixtureOptions(t)
	options.SchemaResolver = &stubProviderSchemaResolver{known: map[string]map[string]struct{}{
		"pagerduty_service_integration": {"integration_key": {}},
		"pagerduty_user":                {"email": {}},
		"aws_sns_topic_subscription":    {"endpoint": {}},
		"aws_ssm_parameter":             {"value": {}},
		"aws_iam_policy":                {"policy": {}},
	}}
	state := `{"serial":17,"lineage":"lineage-123","resources":[
		{"mode":"managed","type":"pagerduty_service_integration","name":"events","instances":[{"attributes":{"id":"PDINT1","integration_key":"secret-routing-key"}}]},
		{"mode":"managed","type":"pagerduty_user","name":"owner","instances":[{"attributes":{"id":"PDUSER1","email":"owner@example.com"}}]},
		{"mode":"managed","type":"aws_sns_topic_subscription","name":"pagerduty","instances":[{"attributes":{"endpoint":"https://events.pagerduty.com/integration/secret-token/enqueue"}}]},
		{"mode":"managed","type":"aws_ssm_parameter","name":"pagerduty_endpoint","instances":[{"attributes":{"value":"https://events.pagerduty.com/integration/secret-token/enqueue"}}]},
		{"mode":"managed","type":"aws_iam_policy","name":"pagerduty_router","instances":[{"attributes":{"policy":"{\"Statement\":[{\"Resource\":\"secret-token\"}]}"} }]}
	]}`

	result, err := terraformstate.Parse(context.Background(), strings.NewReader(state), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	assertNoRawSecret(t, result.Facts, "secret-routing-key")
	assertNoRawSecret(t, result.Facts, "owner@example.com")
	assertNoRawSecret(t, result.Facts, "secret-token")
}
