// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package config

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsConfigMetadataAndRelationships(t *testing.T) {
	client := fakeClient{
		recorders: []ConfigurationRecorder{{
			Name:                       "default",
			AllSupported:               false,
			IncludeGlobalResourceTypes: true,
			RecordingStrategy:          "INCLUSION_BY_RESOURCE_TYPES",
			ResourceTypes:              []string{"AWS::EC2::Instance", "AWS::S3::Bucket"},
		}},
		channels: []DeliveryChannel{{
			Name:                     "default",
			S3BucketName:             "config-bucket",
			S3KeyPrefix:              "prefix",
			S3KMSKeyARN:              "arn:aws-us-gov:kms:us-gov-west-1:123456789012:key/abc",
			SNSTopicARN:              "arn:aws-us-gov:sns:us-gov-west-1:123456789012:config-topic",
			SnapshotDeliveryInterval: "TwentyFour_Hours",
		}},
		rules: []ConfigRule{
			{
				Name:               "s3-bucket-public-read-prohibited",
				ARN:                "arn:aws-us-gov:config:us-gov-west-1:123456789012:config-rule/config-rule-aaaa",
				ID:                 "config-rule-aaaa",
				State:              "ACTIVE",
				Owner:              "AWS",
				SourceIdentifier:   "S3_BUCKET_PUBLIC_READ_PROHIBITED",
				ScopeResourceTypes: []string{"AWS::S3::Bucket"},
			},
			{
				Name:               "custom-lambda-rule",
				ARN:                "arn:aws-us-gov:config:us-gov-west-1:123456789012:config-rule/config-rule-bbbb",
				ID:                 "config-rule-bbbb",
				State:              "ACTIVE",
				Owner:              "CUSTOM_LAMBDA",
				LambdaFunctionARN:  "arn:aws-us-gov:lambda:us-gov-west-1:123456789012:function:config-evaluator",
				ScopeResourceTypes: []string{"AWS::EC2::Instance"},
			},
			{
				Name:             "custom-policy-rule",
				ARN:              "arn:aws-us-gov:config:us-gov-west-1:123456789012:config-rule/config-rule-cccc",
				ID:               "config-rule-cccc",
				State:            "ACTIVE",
				Owner:            "CUSTOM_POLICY",
				SourceIdentifier: "",
			},
		},
		packs: []ConformancePack{{
			Name:      "Operational-Best-Practices",
			ARN:       "arn:aws-us-gov:config:us-gov-west-1:123456789012:conformance-pack/Operational-Best-Practices-abc123",
			ID:        "conformance-pack-abc123",
			Status:    "CREATE_COMPLETE",
			CreatedBy: "",
			RuleNames: []string{"s3-bucket-public-read-prohibited", "encrypted-volumes"},
		}},
		aggregators: []ConfigurationAggregator{{
			Name:             "org-aggregator",
			ARN:              "arn:aws-us-gov:config:us-gov-west-1:123456789012:config-aggregator/config-aggregator-zzzz",
			SourceAccountIDs: []string{"111122223333", "444455556666"},
			SourceRegions:    []string{"us-gov-west-1"},
		}},
		retentions: []RetentionConfiguration{{
			Name:                  "default",
			RetentionPeriodInDays: 2557,
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	recorder := resourceByType(t, envelopes, awscloud.ResourceTypeConfigConfigurationRecorder)
	recorderAttrs := attributesOf(t, recorder)
	if got, want := recorderAttrs["recording_strategy"], "INCLUSION_BY_RESOURCE_TYPES"; got != want {
		t.Fatalf("recorder recording_strategy = %#v, want %q", got, want)
	}
	if types, ok := recorderAttrs["resource_types"].([]string); !ok || len(types) != 2 {
		t.Fatalf("recorder resource_types = %#v, want 2 entries", recorderAttrs["resource_types"])
	}

	channel := resourceByType(t, envelopes, awscloud.ResourceTypeConfigDeliveryChannel)
	if got, want := attributesOf(t, channel)["s3_bucket_name"], "config-bucket"; got != want {
		t.Fatalf("channel s3_bucket_name = %#v, want %q", got, want)
	}

	// The managed rule carries its source identifier and resource-type scope as
	// attributes; the resource-type scope is not a relationship to a synthetic
	// resource-type node.
	managed := resourceByID(t, envelopes, ruleResourceID("s3-bucket-public-read-prohibited"))
	managedAttrs := attributesOf(t, managed)
	if got, want := managedAttrs["owner"], "AWS"; got != want {
		t.Fatalf("managed rule owner = %#v, want %q", got, want)
	}
	if got, want := managedAttrs["source_identifier"], "S3_BUCKET_PUBLIC_READ_PROHIBITED"; got != want {
		t.Fatalf("managed rule source_identifier = %#v, want %q", got, want)
	}
	if scope, ok := managedAttrs["scope_resource_types"].([]string); !ok || !slices.Equal(scope, []string{"AWS::S3::Bucket"}) {
		t.Fatalf("managed rule scope_resource_types = %#v, want [AWS::S3::Bucket]", managedAttrs["scope_resource_types"])
	}

	// Exactly one custom-rule-to-Lambda relationship, only for the CUSTOM_LAMBDA
	// rule, targeting the Lambda function ARN (aws_lambda_function).
	assertRelationshipCount(t, envelopes, awscloud.RelationshipConfigRuleEvaluatedByLambda, 1)
	lambdaRel := relationshipByType(t, envelopes, awscloud.RelationshipConfigRuleEvaluatedByLambda)
	if got, want := lambdaRel.Payload["target_type"], awscloud.ResourceTypeLambdaFunction; got != want {
		t.Fatalf("lambda relationship target_type = %#v, want %q", got, want)
	}
	if got, want := lambdaRel.Payload["target_resource_id"], "arn:aws-us-gov:lambda:us-gov-west-1:123456789012:function:config-evaluator"; got != want {
		t.Fatalf("lambda relationship target_resource_id = %#v, want %q", got, want)
	}
	if got, want := lambdaRel.Payload["source_resource_id"], ruleResourceID("custom-lambda-rule"); got != want {
		t.Fatalf("lambda relationship source_resource_id = %#v, want %q", got, want)
	}

	// Conformance pack carries a rule count and one containment edge per member
	// rule, each targeting the aws_config_rule node by rule name.
	pack := resourceByType(t, envelopes, awscloud.ResourceTypeConfigConformancePack)
	if got, want := attributesOf(t, pack)["rule_count"], 2; got != want {
		t.Fatalf("conformance pack rule_count = %#v, want %d", got, want)
	}
	if got, want := pack.Payload["state"], "CREATE_COMPLETE"; got != want {
		t.Fatalf("conformance pack state = %#v, want %q", got, want)
	}
	assertRelationshipCount(t, envelopes, awscloud.RelationshipConfigConformancePackContainsRule, 2)
	packRel := relationshipByTypeAndTarget(t, envelopes, awscloud.RelationshipConfigConformancePackContainsRule, ruleResourceID("s3-bucket-public-read-prohibited"))
	if got, want := packRel.Payload["target_type"], awscloud.ResourceTypeConfigRule; got != want {
		t.Fatalf("conformance pack rule target_type = %#v, want %q", got, want)
	}

	// Aggregator carries source accounts and emits one account edge per source,
	// targeting the account root ARN with the partition derived from the
	// aggregator ARN (aws-us-gov here, not a hardcoded aws partition).
	aggregator := resourceByType(t, envelopes, awscloud.ResourceTypeConfigConfigurationAggregator)
	if got, want := attributesOf(t, aggregator)["source_account_count"], 2; got != want {
		t.Fatalf("aggregator source_account_count = %#v, want %d", got, want)
	}
	assertRelationshipCount(t, envelopes, awscloud.RelationshipConfigAggregatorSourcesAccount, 2)
	acctRel := relationshipByTypeAndTarget(t, envelopes, awscloud.RelationshipConfigAggregatorSourcesAccount, "arn:aws-us-gov:iam::111122223333:root")
	if got, want := acctRel.Payload["target_type"], awscloud.ResourceTypeAWSAccount; got != want {
		t.Fatalf("aggregator account target_type = %#v, want %q", got, want)
	}

	retention := resourceByType(t, envelopes, awscloud.ResourceTypeConfigRetentionConfiguration)
	if got, want := attributesOf(t, retention)["retention_period_in_days"], int32(2557); got != want {
		t.Fatalf("retention retention_period_in_days = %#v, want %d", got, want)
	}

	// Every relationship target must reference a real, emitted resource node or a
	// cross-service account target. config-rule targets join to emitted rule
	// nodes; the account and Lambda targets are intentional cross-service edges.
	assertNoDanglingConfigRuleTargets(t, envelopes)
	for _, envelope := range envelopes {
		assertNoConfigItemBody(t, envelope)
		assertTargetTypeSet(t, envelope)
	}
}

// TestScannerDerivesPartitionFromARN proves the aggregator-to-account edge never
// hardcodes the commercial "aws" partition. A China-partition aggregator must
// yield a China account root ARN.
func TestScannerDerivesPartitionFromARN(t *testing.T) {
	client := fakeClient{
		aggregators: []ConfigurationAggregator{{
			Name:             "cn-aggregator",
			ARN:              "arn:aws-cn:config:cn-north-1:123456789012:config-aggregator/config-aggregator-cn",
			SourceAccountIDs: []string{"111122223333"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	rel := relationshipByType(t, envelopes, awscloud.RelationshipConfigAggregatorSourcesAccount)
	if got, want := rel.Payload["target_resource_id"], "arn:aws-cn:iam::111122223333:root"; got != want {
		t.Fatalf("aggregator account target = %#v, want %q (China partition derived from ARN)", got, want)
	}
}

// TestScannerSkipsAggregatorAccountWhenPartitionUnknown proves the scanner does
// not synthesize an account edge with a guessed partition when the aggregator
// ARN is missing.
func TestScannerSkipsAggregatorAccountWhenPartitionUnknown(t *testing.T) {
	client := fakeClient{
		aggregators: []ConfigurationAggregator{{
			Name:             "no-arn-aggregator",
			SourceAccountIDs: []string{"111122223333"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := relationshipCount(envelopes, awscloud.RelationshipConfigAggregatorSourcesAccount); got != 0 {
		t.Fatalf("aggregator account relationship count = %d, want 0 when partition cannot be derived", got)
	}
}

// TestScannerEmitsNoLambdaEdgeForManagedOrPolicyRules guards that only custom
// Lambda rules emit a Lambda relationship.
func TestScannerEmitsNoLambdaEdgeForManagedOrPolicyRules(t *testing.T) {
	client := fakeClient{
		rules: []ConfigRule{
			{Name: "managed", ARN: "arn:aws:config:us-east-1:123456789012:config-rule/r1", Owner: "AWS", SourceIdentifier: "REQUIRED_TAGS"},
			{Name: "policy", ARN: "arn:aws:config:us-east-1:123456789012:config-rule/r2", Owner: "CUSTOM_POLICY"},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := relationshipCount(envelopes, awscloud.RelationshipConfigRuleEvaluatedByLambda); got != 0 {
		t.Fatalf("Lambda relationship count = %d, want 0 for managed and policy rules", got)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceGuardDuty

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

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-gov-west-1",
		ServiceKind:         awscloud.ServiceConfig,
		ScopeID:             "aws:123456789012:us-gov-west-1",
		GenerationID:        "aws:123456789012:us-gov-west-1:config:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	recorders   []ConfigurationRecorder
	channels    []DeliveryChannel
	rules       []ConfigRule
	packs       []ConformancePack
	aggregators []ConfigurationAggregator
	retentions  []RetentionConfiguration
}

func (c fakeClient) ConfigurationRecorders(context.Context) ([]ConfigurationRecorder, error) {
	return c.recorders, nil
}

func (c fakeClient) DeliveryChannels(context.Context) ([]DeliveryChannel, error) {
	return c.channels, nil
}

func (c fakeClient) ConfigRules(context.Context) ([]ConfigRule, error) {
	return c.rules, nil
}

func (c fakeClient) ConformancePacks(context.Context) ([]ConformancePack, error) {
	return c.packs, nil
}

func (c fakeClient) ConfigurationAggregators(context.Context) ([]ConfigurationAggregator, error) {
	return c.aggregators, nil
}

func (c fakeClient) RetentionConfigurations(context.Context) ([]RetentionConfiguration, error) {
	return c.retentions, nil
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
	t.Fatalf("missing resource_type %q", resourceType)
	return facts.Envelope{}
}

func resourceByID(t *testing.T, envelopes []facts.Envelope, resourceID string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_id"].(string); got == resourceID {
			return envelope
		}
	}
	t.Fatalf("missing resource_id %q", resourceID)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q", relationshipType)
	return facts.Envelope{}
}

func relationshipByTypeAndTarget(t *testing.T, envelopes []facts.Envelope, relationshipType, target string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		gotType, _ := envelope.Payload["relationship_type"].(string)
		gotTarget, _ := envelope.Payload["target_resource_id"].(string)
		if gotType == relationshipType && gotTarget == target {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q with target %q", relationshipType, target)
	return facts.Envelope{}
}

func assertRelationshipCount(t *testing.T, envelopes []facts.Envelope, relationshipType string, want int) {
	t.Helper()
	if got := relationshipCount(envelopes, relationshipType); got != want {
		t.Fatalf("relationship_type %q count = %d, want %d", relationshipType, got, want)
	}
}

func relationshipCount(envelopes []facts.Envelope, relationshipType string) int {
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			count++
		}
	}
	return count
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

// assertTargetTypeSet fails if any relationship omits target_type. A dangling
// edge with an empty target type would mislead downstream graph consumers.
func assertTargetTypeSet(t *testing.T, envelope facts.Envelope) {
	t.Helper()
	if envelope.FactKind != facts.AWSRelationshipFactKind {
		return
	}
	if got, _ := envelope.Payload["target_type"].(string); strings.TrimSpace(got) == "" {
		t.Fatalf("relationship has empty target_type: %#v", envelope)
	}
}

// assertNoDanglingConfigRuleTargets confirms every conformance-pack-to-rule edge
// targets a config-rule resource_id (the rule node convention), so the edge
// joins to a real aws_config_rule node rather than a synthetic resource-type
// string.
func assertNoDanglingConfigRuleTargets(t *testing.T, envelopes []facts.Envelope) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != awscloud.RelationshipConfigConformancePackContainsRule {
			continue
		}
		target, _ := envelope.Payload["target_resource_id"].(string)
		if !strings.HasPrefix(target, "config-rule/") {
			t.Fatalf("conformance pack rule edge targets %q, want config-rule/ resource id", target)
		}
	}
}

// assertNoConfigItemBody fails if any emitted payload carries a recorded
// configuration item body field. AWS Config configuration item bodies are full
// resource snapshots and must never be persisted by this scanner.
func assertNoConfigItemBody(t *testing.T, envelope facts.Envelope) {
	t.Helper()
	forbidden := []string{
		"configuration", "configuration_item", "configuration_item_md5_hash",
		"supplementary_configuration", "relationships_body", "resource_config",
		"config_item", "configuration_item_status",
	}
	if attrs, ok := envelope.Payload["attributes"].(map[string]any); ok {
		for key := range attrs {
			for _, banned := range forbidden {
				if key == banned {
					t.Fatalf("payload carries forbidden config-item-body field %q: %#v", key, envelope)
				}
			}
		}
	}
}
