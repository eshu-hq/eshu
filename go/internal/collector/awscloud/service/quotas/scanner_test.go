// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicequotas

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testQuotaARN = "arn:aws:servicequotas:us-east-1:123456789012:ec2/L-1216C47A"
)

func floatPtr(v float64) *float64 { return &v }
func int32Ptr(v int32) *int32     { return &v }

func TestScannerEmitsAppliedQuotaMetadata(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Quotas: []ServiceQuota{{
		ARN:          testQuotaARN,
		ServiceCode:  "ec2",
		ServiceName:  "Amazon Elastic Compute Cloud (Amazon EC2)",
		QuotaCode:    "L-1216C47A",
		QuotaName:    "Running On-Demand Standard instances",
		Description:  "Maximum vCPUs for On-Demand Standard instances",
		AppliedValue: floatPtr(256),
		DefaultValue: floatPtr(5),
		Overridden:   true,
		Adjustable:   true,
		GlobalQuota:  false,
		Unit:         "None",
		AppliedLevel: "ACCOUNT",
		UsageMetric: &UsageMetric{
			Namespace:               "AWS/Usage",
			Name:                    "ResourceCount",
			Dimensions:              map[string]string{"Type": "Resource", "Class": "Standard/OnDemand"},
			StatisticRecommendation: "Maximum",
		},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	quota := resourceByType(t, envelopes, awscloud.ResourceTypeServiceQuotasServiceQuota)
	if got, want := quota.Payload["resource_id"], testQuotaARN; got != want {
		t.Fatalf("quota resource_id = %#v, want %q", got, want)
	}
	if got, want := quota.Payload["arn"], testQuotaARN; got != want {
		t.Fatalf("quota arn = %#v, want %q", got, want)
	}
	if got, want := quota.Payload["name"], "Running On-Demand Standard instances"; got != want {
		t.Fatalf("quota name = %#v, want %q", got, want)
	}
	attrs := attributesOf(t, quota)
	assertAttribute(t, attrs, "service_code", "ec2")
	assertAttribute(t, attrs, "quota_code", "L-1216C47A")
	assertAttribute(t, attrs, "applied_value", float64(256))
	assertAttribute(t, attrs, "default_value", float64(5))
	assertAttribute(t, attrs, "overridden", true)
	assertAttribute(t, attrs, "adjustable", true)
	assertAttribute(t, attrs, "applied_level", "ACCOUNT")

	metric, ok := attrs["usage_metric"].(map[string]any)
	if !ok {
		t.Fatalf("usage_metric = %#v, want map", attrs["usage_metric"])
	}
	if got, want := metric["metric_namespace"], "AWS/Usage"; got != want {
		t.Fatalf("metric_namespace = %#v, want %q", got, want)
	}
	if got, want := metric["statistic_recommendation"], "Maximum"; got != want {
		t.Fatalf("statistic_recommendation = %#v, want %q", got, want)
	}
}

func TestScannerEmitsNoRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Quotas: []ServiceQuota{{
		ARN:          testQuotaARN,
		ServiceCode:  "ec2",
		QuotaCode:    "L-1216C47A",
		AppliedValue: floatPtr(256),
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("servicequotas scanner emitted a relationship; quotas reference a service code, not a scanned resource: %#v", envelope.Payload)
		}
	}
}

func TestScannerMarksOnlyDivergedQuotaAsOverridden(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Quotas: []ServiceQuota{
		{
			ARN:          "arn:aws:servicequotas:us-east-1:123456789012:ec2/L-AT-DEFAULT",
			ServiceCode:  "ec2",
			QuotaCode:    "L-AT-DEFAULT",
			AppliedValue: floatPtr(5),
			DefaultValue: floatPtr(5),
			Overridden:   false,
		},
		{
			ARN:          "arn:aws:servicequotas:us-east-1:123456789012:ec2/L-RAISED",
			ServiceCode:  "ec2",
			QuotaCode:    "L-RAISED",
			AppliedValue: floatPtr(256),
			DefaultValue: floatPtr(5),
			Overridden:   true,
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	overrides := map[string]bool{}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		code, _ := attrs["quota_code"].(string)
		overridden, _ := attrs["overridden"].(bool)
		overrides[code] = overridden
	}
	if overrides["L-AT-DEFAULT"] {
		t.Fatalf("at-default quota marked overridden")
	}
	if !overrides["L-RAISED"] {
		t.Fatalf("raised quota not marked overridden")
	}
}

func TestScannerOmitsUnknownValuesAndOptionalAttributes(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Quotas: []ServiceQuota{{
		ARN:         testQuotaARN,
		ServiceCode: "ec2",
		QuotaCode:   "L-1216C47A",
		// No applied/default value, no usage metric, no context, no period.
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	quota := resourceByType(t, envelopes, awscloud.ResourceTypeServiceQuotasServiceQuota)
	attrs := attributesOf(t, quota)
	if got := attrs["applied_value"]; got != nil {
		t.Fatalf("applied_value = %#v, want nil for unknown value", got)
	}
	if got := attrs["default_value"]; got != nil {
		t.Fatalf("default_value = %#v, want nil for unknown value", got)
	}
	if got := attrs["overridden"]; got != false {
		t.Fatalf("overridden = %#v, want false when values are unknown", got)
	}
	if got := attrs["usage_metric"]; got != nil {
		t.Fatalf("usage_metric = %#v, want nil when AWS reports none", got)
	}
	if got := attrs["quota_context"]; got != nil {
		t.Fatalf("quota_context = %#v, want nil for account-level quota", got)
	}
	if got := attrs["period_value"]; got != nil {
		t.Fatalf("period_value = %#v, want nil for non-rate quota", got)
	}
}

func TestScannerEmitsResourceLevelQuotaContext(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Quotas: []ServiceQuota{{
		ARN:          "arn:aws:servicequotas:us-east-1:123456789012:es/L-CONTEXT",
		ServiceCode:  "es",
		QuotaCode:    "L-CONTEXT",
		AppliedValue: floatPtr(10),
		AppliedLevel: "RESOURCE",
		PeriodUnit:   "SECOND",
		PeriodValue:  int32Ptr(1),
		QuotaContext: &QuotaContext{
			ContextID:        "*",
			ContextScope:     "RESOURCE",
			ContextScopeType: "AWS::OpenSearchService::Domain",
		},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	quota := resourceByType(t, envelopes, awscloud.ResourceTypeServiceQuotasServiceQuota)
	attrs := attributesOf(t, quota)
	assertAttribute(t, attrs, "period_unit", "SECOND")
	assertAttribute(t, attrs, "period_value", int32(1))
	context, ok := attrs["quota_context"].(map[string]any)
	if !ok {
		t.Fatalf("quota_context = %#v, want map", attrs["quota_context"])
	}
	if got, want := context["context_scope_type"], "AWS::OpenSearchService::Domain"; got != want {
		t.Fatalf("context_scope_type = %#v, want %q", got, want)
	}
}

func TestScannerFallsBackToServiceQuotaKeyWhenARNMissing(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Quotas: []ServiceQuota{{
		ServiceCode:  "lambda",
		QuotaCode:    "L-B99A9384",
		AppliedValue: floatPtr(1000),
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	quota := resourceByType(t, envelopes, awscloud.ResourceTypeServiceQuotasServiceQuota)
	if got, want := quota.Payload["resource_id"], "lambda/L-B99A9384"; got != want {
		t.Fatalf("resource_id = %#v, want %q", got, want)
	}
	if got := quota.Payload["arn"]; got != "" {
		t.Fatalf("arn = %#v, want empty when AWS reports no quota ARN", got)
	}
}

func TestScannerCanonicalizesServiceKindForPaddedInput(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceServiceQuotas
	client := fakeClient{}
	if _, err := (Scanner{Client: client}).Scan(context.Background(), boundary); err != nil {
		t.Fatalf("Scan() error = %v, want nil for canonical service_kind", err)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Quotas: []ServiceQuota{{ARN: testQuotaARN, ServiceCode: "ec2", QuotaCode: "L-1216C47A"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Service Quotas ListServiceQuotas throttled after SDK retries; quota metadata omitted for this scan",
			SourceRecordID: "servicequotas_throttled",
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	warning := warningByKind(t, envelopes, awscloud.WarningThrottleSustained)
	if got := warning.Payload["error_class"]; got != "throttled" {
		t.Fatalf("warning error_class = %#v, want throttled", got)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceServiceQuotas,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:servicequotas:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	snapshot Snapshot
}

func (c fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return c.snapshot, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func warningByKind(t *testing.T, envelopes []facts.Envelope, warningKind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSWarningFactKind {
			continue
		}
		if got, _ := envelope.Payload["warning_kind"].(string); got == warningKind {
			return envelope
		}
	}
	t.Fatalf("missing warning_kind %q in %#v", warningKind, envelopes)
	return facts.Envelope{}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttribute(t *testing.T, attributes map[string]any, key string, want any) {
	t.Helper()
	got, exists := attributes[key]
	if !exists {
		t.Fatalf("missing attribute %q in %#v", key, attributes)
	}
	if got != want {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}
