// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package xray

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsGroupsRulesAndEncryptionConfig(t *testing.T) {
	groupARN := "arn:aws:xray:us-east-1:123456789012:group/orders/abc123"
	ruleARN := "arn:aws:xray:us-east-1:123456789012:sampling-rule/orders-rule"
	keyARN := "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
	priority := int32(1000)
	version := int32(1)
	insightsOn := true
	client := fakeClient{
		groups: []Group{{
			ARN:              groupARN,
			Name:             "orders",
			FilterExpression: `service("orders-api")`,
			InsightsEnabled:  &insightsOn,
		}},
		rules: []SamplingRule{{
			ARN:           ruleARN,
			Name:          "orders-rule",
			Priority:      &priority,
			ReservoirSize: 5,
			FixedRate:     0.1,
			ServiceName:   "orders-api",
			ServiceType:   "AWS::ECS::Container",
			Host:          "*",
			HTTPMethod:    "*",
			URLPath:       "*",
			Version:       &version,
		}},
		config: &EncryptionConfig{
			Type:   "KMS",
			Status: "ACTIVE",
			KeyID:  keyARN,
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	for _, kind := range []string{
		awscloud.ResourceTypeXRayGroup,
		awscloud.ResourceTypeXRaySamplingRule,
		awscloud.ResourceTypeXRayEncryptionConfig,
	} {
		if _, ok := firstResource(envelopes, kind); !ok {
			t.Fatalf("missing resource_type %q in envelopes", kind)
		}
	}

	// Group carries the filter expression as configuration.
	group, _ := firstResource(envelopes, awscloud.ResourceTypeXRayGroup)
	groupAttrs := attributesOf(t, group)
	if got, want := groupAttrs["filter_expression"], `service("orders-api")`; got != want {
		t.Fatalf("group filter_expression = %#v, want %q", got, want)
	}
	if got, want := groupAttrs["insights_enabled"], true; got != want {
		t.Fatalf("group insights_enabled = %#v, want %v", got, want)
	}
	if group.Payload["resource_id"] != groupARN {
		t.Fatalf("group resource_id = %#v, want %q", group.Payload["resource_id"], groupARN)
	}

	// Sampling rule carries the priority/reservoir/rate configuration.
	rule, _ := firstResource(envelopes, awscloud.ResourceTypeXRaySamplingRule)
	ruleAttrs := attributesOf(t, rule)
	if got, want := ruleAttrs["priority"], int32(1000); got != want {
		t.Fatalf("rule priority = %#v, want %v", got, want)
	}
	if got, want := ruleAttrs["fixed_rate"], 0.1; got != want {
		t.Fatalf("rule fixed_rate = %#v, want %v", got, want)
	}
	if got, want := ruleAttrs["service_name"], "orders-api"; got != want {
		t.Fatalf("rule service_name = %#v, want %q", got, want)
	}

	// Encryption config resource id is account/region scoped.
	config, _ := firstResource(envelopes, awscloud.ResourceTypeXRayEncryptionConfig)
	wantConfigID := "123456789012/us-east-1/xray-encryption-config"
	if config.Payload["resource_id"] != wantConfigID {
		t.Fatalf("encryption config resource_id = %#v, want %q", config.Payload["resource_id"], wantConfigID)
	}
}

func TestEncryptionConfigKMSEdgeJoinsKMSKey(t *testing.T) {
	keyARN := "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
	client := fakeClient{
		config: &EncryptionConfig{Type: "KMS", Status: "ACTIVE", KeyID: keyARN},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	edges := relationshipsOfType(envelopes, awscloud.RelationshipXRayEncryptionConfigUsesKMSKey)
	if got, want := len(edges), 1; got != want {
		t.Fatalf("kms edges = %d, want %d", got, want)
	}
	edge := edges[0]
	// The target_type must be the family the KMS scanner publishes, or the edge
	// dangles. Regression for the #804 graph-join defect class.
	if got, want := edge.Payload["target_type"], awscloud.ResourceTypeKMSKey; got != want {
		t.Fatalf("kms edge target_type = %#v, want %q", got, want)
	}
	// The KMS scanner keys its key resource_id as the bare key id, falling back
	// to the key ARN; the reported reference is used as the join key directly.
	if got, want := edge.Payload["target_resource_id"], keyARN; got != want {
		t.Fatalf("kms edge target_resource_id = %#v, want %q", got, want)
	}
	if got, want := edge.Payload["target_arn"], keyARN; got != want {
		t.Fatalf("kms edge target_arn = %#v, want %q", got, want)
	}
}

func TestEncryptionConfigKMSEdgeKeysBareIDWithoutFabricatedARN(t *testing.T) {
	// A bare key id (or alias) must not be given a fabricated ARN, or the
	// relguard join-mode check would flag an ARN-keyed target keyed by a name.
	boundary := testBoundary()
	edge, ok := encryptionConfigKMSRelationship(boundary, EncryptionConfig{
		Type:   "KMS",
		Status: "ACTIVE",
		KeyID:  "1234abcd-12ab-34cd-56ef-1234567890ab",
	})
	if !ok {
		t.Fatal("expected a KMS edge for a bare key id")
	}
	if edge.TargetARN != "" {
		t.Fatalf("bare key id given a fabricated target_arn = %q", edge.TargetARN)
	}
	if edge.TargetResourceID != "1234abcd-12ab-34cd-56ef-1234567890ab" {
		t.Fatalf("target_resource_id = %q, want the bare key id", edge.TargetResourceID)
	}
}

func TestEncryptionConfigKMSEdgeTargetsAliasForAliasName(t *testing.T) {
	// X-Ray PutEncryptionConfig accepts a KMS alias name ("alias/MyKey"), and
	// GetEncryptionConfig reports the reference verbatim. The KMS scanner emits
	// aliases as aws_kms_alias nodes keyed by firstNonEmpty(aliasARN, aliasName),
	// so an alias-name reference must target aws_kms_alias by that bare name; a
	// fabricated key-family target would dangle. Regression for the #804
	// graph-join defect class.
	boundary := testBoundary()
	edge, ok := encryptionConfigKMSRelationship(boundary, EncryptionConfig{
		Type:   "KMS",
		Status: "ACTIVE",
		KeyID:  "alias/MyKey",
	})
	if !ok {
		t.Fatal("expected a KMS edge for an alias-name reference")
	}
	if got, want := edge.TargetType, awscloud.ResourceTypeKMSAlias; got != want {
		t.Fatalf("alias-name edge target_type = %q, want %q", got, want)
	}
	if got, want := edge.TargetResourceID, "alias/MyKey"; got != want {
		t.Fatalf("alias-name edge target_resource_id = %q, want %q", got, want)
	}
	if edge.TargetARN != "" {
		t.Fatalf("alias name given a fabricated target_arn = %q", edge.TargetARN)
	}
}

func TestEncryptionConfigKMSEdgeTargetsAliasForAliasARN(t *testing.T) {
	// An alias ARN ("arn:aws:kms:...:alias/MyKey") must target aws_kms_alias by
	// the ARN, matching the alias scanner's resource_id = firstNonEmpty(aliasARN,
	// aliasName). A key ARN ("...:key/...") still targets aws_kms_key; only the
	// alias-shaped resource segment switches families.
	boundary := testBoundary()
	edge, ok := encryptionConfigKMSRelationship(boundary, EncryptionConfig{
		Type:   "KMS",
		Status: "ACTIVE",
		KeyID:  "arn:aws:kms:us-east-1:123456789012:alias/MyKey",
	})
	if !ok {
		t.Fatal("expected a KMS edge for an alias-ARN reference")
	}
	if got, want := edge.TargetType, awscloud.ResourceTypeKMSAlias; got != want {
		t.Fatalf("alias-ARN edge target_type = %q, want %q", got, want)
	}
	if got, want := edge.TargetResourceID, "arn:aws:kms:us-east-1:123456789012:alias/MyKey"; got != want {
		t.Fatalf("alias-ARN edge target_resource_id = %q, want %q", got, want)
	}
	if got, want := edge.TargetARN, "arn:aws:kms:us-east-1:123456789012:alias/MyKey"; got != want {
		t.Fatalf("alias-ARN edge target_arn = %q, want %q", got, want)
	}
	// The graph-join contract must hold for the alias-targeted edge.
	relguard.AssertObservations(t, edge)
}

func TestNoKMSEdgeWhenEncryptionTypeIsNone(t *testing.T) {
	client := fakeClient{
		config: &EncryptionConfig{Type: "NONE", Status: "ACTIVE"},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if edges := relationshipsOfType(envelopes, awscloud.RelationshipXRayEncryptionConfigUsesKMSKey); len(edges) != 0 {
		t.Fatalf("kms edges = %d, want 0 for NONE encryption", len(edges))
	}
}

func TestSamplingRuleServiceCorrelationEdge(t *testing.T) {
	ruleARN := "arn:aws:xray:us-east-1:123456789012:sampling-rule/orders-rule"
	client := fakeClient{
		rules: []SamplingRule{{
			ARN:         ruleARN,
			Name:        "orders-rule",
			ServiceName: "orders-api",
			ServiceType: "AWS::ECS::Container",
		}},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	edges := relationshipsOfType(envelopes, awscloud.RelationshipXRaySamplingRuleMatchesService)
	if got, want := len(edges), 1; got != want {
		t.Fatalf("service correlation edges = %d, want %d", got, want)
	}
	edge := edges[0]
	if got, want := edge.Payload["target_type"], awscloud.ResourceTypeXRayServiceCorrelation; got != want {
		t.Fatalf("service edge target_type = %#v, want %q", got, want)
	}
	if got, want := edge.Payload["target_resource_id"], "orders-api/AWS::ECS::Container"; got != want {
		t.Fatalf("service edge target_resource_id = %#v, want %q", got, want)
	}
	// A correlation anchor is name-keyed, never ARN-keyed.
	if got := edge.Payload["target_arn"]; got != "" && got != nil {
		t.Fatalf("service correlation edge target_arn = %#v, want empty", got)
	}
}

func TestWildcardSamplingRuleEmitsNoServiceEdge(t *testing.T) {
	client := fakeClient{
		rules: []SamplingRule{{
			ARN:         "arn:aws:xray:us-east-1:123456789012:sampling-rule/default",
			Name:        "Default",
			ServiceName: "*",
			ServiceType: "*",
		}},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if edges := relationshipsOfType(envelopes, awscloud.RelationshipXRaySamplingRuleMatchesService); len(edges) != 0 {
		t.Fatalf("service edges = %d, want 0 for a wildcard-only rule", len(edges))
	}
	// The rule resource itself is still emitted.
	if _, ok := firstResource(envelopes, awscloud.ResourceTypeXRaySamplingRule); !ok {
		t.Fatal("wildcard rule resource missing")
	}
}

func TestEmittedRelationshipsSatisfyGraphJoinContract(t *testing.T) {
	boundary := testBoundary()

	kmsEdge, ok := encryptionConfigKMSRelationship(boundary, EncryptionConfig{
		Type:   "KMS",
		Status: "ACTIVE",
		KeyID:  "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab",
	})
	if !ok {
		t.Fatal("encryptionConfigKMSRelationship did not emit an edge for a KMS key")
	}
	serviceEdge, ok := samplingRuleServiceRelationship(boundary, SamplingRule{
		ARN:         "arn:aws:xray:us-east-1:123456789012:sampling-rule/orders-rule",
		Name:        "orders-rule",
		ServiceName: "orders-api",
		ServiceType: "AWS::ECS::Container",
	})
	if !ok {
		t.Fatal("samplingRuleServiceRelationship did not emit an edge for a named service")
	}
	relguard.AssertObservations(t, kmsEdge, serviceEdge)
}

// TestScannerEmitsNoObservabilityPayload is the config-only exclusion test. It
// reflects over the scanner-owned Client interface and asserts the interface
// exposes exactly the three configuration reads and that NO trace, service-map,
// insight, telemetry, or mutation method is present. This proves at compile
// time that the scanner cannot read X-Ray observability payload.
func TestScannerEmitsNoObservabilityPayload(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil)).Elem()

	allowed := map[string]struct{}{
		"GetGroups":           {},
		"GetSamplingRules":    {},
		"GetEncryptionConfig": {},
	}
	if got, want := clientType.NumMethod(), len(allowed); got != want {
		var names []string
		for i := 0; i < clientType.NumMethod(); i++ {
			names = append(names, clientType.Method(i).Name)
		}
		t.Fatalf("Client exposes %d methods %v, want exactly %d config reads", got, names, want)
	}
	for i := 0; i < clientType.NumMethod(); i++ {
		name := clientType.Method(i).Name
		if _, ok := allowed[name]; !ok {
			t.Fatalf("Client exposes unexpected method %q; only config reads are allowed", name)
		}
	}

	// Belt and suspenders: assert each known observability/mutation method is
	// absent by name, so a future edit that adds one fails loudly.
	forbidden := []string{
		"GetTraceSummaries", "BatchGetTraces", "GetTraceGraph", "GetServiceGraph",
		"GetTimeSeriesServiceStatistics", "GetInsight", "GetInsightSummaries",
		"GetInsightEvents", "GetInsightImpactGraph", "GetSamplingTargets",
		"GetSamplingStatisticSummaries", "PutTraceSegments", "PutTelemetryRecords",
		"CreateGroup", "UpdateGroup", "DeleteGroup", "CreateSamplingRule",
		"UpdateSamplingRule", "DeleteSamplingRule", "PutEncryptionConfig",
	}
	for _, name := range forbidden {
		if _, ok := clientType.MethodByName(name); ok {
			t.Fatalf("Client exposes forbidden observability/mutation method %q", name)
		}
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatal("Scan() error = nil, want missing client")
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceKMS
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatal("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerDefaultsServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = ""
	if _, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary); err != nil {
		t.Fatalf("Scan() error = %v, want nil for empty service kind", err)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceXRay,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:xray:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 16, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	groups []Group
	rules  []SamplingRule
	config *EncryptionConfig
}

func (c fakeClient) GetGroups(context.Context) ([]Group, error) {
	return c.groups, nil
}

func (c fakeClient) GetSamplingRules(context.Context) ([]SamplingRule, error) {
	return c.rules, nil
}

func (c fakeClient) GetEncryptionConfig(context.Context) (*EncryptionConfig, error) {
	return c.config, nil
}

func firstResource(envelopes []facts.Envelope, resourceType string) (facts.Envelope, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope, true
		}
	}
	return facts.Envelope{}, false
}

func relationshipsOfType(envelopes []facts.Envelope, relationshipType string) []facts.Envelope {
	var out []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			out = append(out, envelope)
		}
	}
	return out
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
