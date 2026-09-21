// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appconfig

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testAppID      = "app123"
	testEnvID      = "env456"
	testProfileID  = "prof789"
	testStrategyID = "strat012"
	testAlarmARN   = "arn:aws:cloudwatch:us-east-1:123456789012:alarm:high-error-rate"
	testRoleARN    = "arn:aws:iam::123456789012:role/appconfig-monitor"
)

var (
	wantAppARN      = "arn:aws:appconfig:us-east-1:123456789012:application/" + testAppID
	wantEnvARN      = "arn:aws:appconfig:us-east-1:123456789012:application/" + testAppID + "/environment/" + testEnvID
	wantProfileARN  = "arn:aws:appconfig:us-east-1:123456789012:application/" + testAppID + "/configurationprofile/" + testProfileID
	wantStrategyARN = "arn:aws:appconfig:us-east-1:123456789012:deploymentstrategy/" + testStrategyID
)

func fullFixture() Snapshot {
	return Snapshot{
		Applications: []Application{{
			ID:          testAppID,
			Name:        "checkout",
			Description: "checkout service config",
			Tags:        map[string]string{"Team": "payments"},
			Environments: []Environment{{
				ID:            testEnvID,
				ApplicationID: testAppID,
				Name:          "prod",
				State:         "READY_FOR_DEPLOYMENT",
				Monitors: []Monitor{{
					AlarmARN:     testAlarmARN,
					AlarmRoleARN: testRoleARN,
				}},
				Tags: map[string]string{"Stage": "prod"},
			}},
			Profiles: []ConfigurationProfile{{
				ID:             testProfileID,
				ApplicationID:  testAppID,
				Name:           "feature-flags",
				Type:           "AWS.AppConfig.FeatureFlags",
				LocationURI:    "hosted",
				ValidatorTypes: []string{"JSON_SCHEMA"},
			}},
		}},
		DeploymentStrategies: []DeploymentStrategy{{
			ID:                          testStrategyID,
			Name:                        "Canary10Percent20Minutes",
			DeploymentDurationInMinutes: 20,
			FinalBakeTimeInMinutes:      10,
			GrowthFactor:                10,
			GrowthType:                  "EXPONENTIAL",
			ReplicateTo:                 "SSM_DOCUMENT",
		}},
	}
}

func TestScannerEmitsAppConfigMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: fullFixture()}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Application resource node, keyed by the synthesized application ARN.
	application := resourceByType(t, envelopes, awscloud.ResourceTypeAppConfigApplication)
	if got, want := application.Payload["resource_id"], wantAppARN; got != want {
		t.Fatalf("application resource_id = %#v, want %q", got, want)
	}
	if got, want := application.Payload["arn"], wantAppARN; got != want {
		t.Fatalf("application arn = %#v, want %q", got, want)
	}
	appAttrs := attributesOf(t, application)
	assertAttribute(t, appAttrs, "application_id", testAppID)

	// Environment resource node.
	environment := resourceByType(t, envelopes, awscloud.ResourceTypeAppConfigEnvironment)
	if got, want := environment.Payload["resource_id"], wantEnvARN; got != want {
		t.Fatalf("environment resource_id = %#v, want %q", got, want)
	}
	if got, want := environment.Payload["state"], "READY_FOR_DEPLOYMENT"; got != want {
		t.Fatalf("environment state = %#v, want %q", got, want)
	}
	envAttrs := attributesOf(t, environment)
	assertAttribute(t, envAttrs, "monitor_count", int64(1))
	assertAttribute(t, envAttrs, "application_id", testAppID)

	// Configuration profile resource node.
	profile := resourceByType(t, envelopes, awscloud.ResourceTypeAppConfigConfigurationProfile)
	if got, want := profile.Payload["resource_id"], wantProfileARN; got != want {
		t.Fatalf("profile resource_id = %#v, want %q", got, want)
	}
	profAttrs := attributesOf(t, profile)
	assertAttribute(t, profAttrs, "profile_type", "AWS.AppConfig.FeatureFlags")
	assertAttribute(t, profAttrs, "location_uri", "hosted")
	assertAttribute(t, profAttrs, "validator_types", []string{"JSON_SCHEMA"})

	// Deployment strategy resource node (account-level).
	strategy := resourceByType(t, envelopes, awscloud.ResourceTypeAppConfigDeploymentStrategy)
	if got, want := strategy.Payload["resource_id"], wantStrategyARN; got != want {
		t.Fatalf("strategy resource_id = %#v, want %q", got, want)
	}
	stratAttrs := attributesOf(t, strategy)
	assertAttribute(t, stratAttrs, "deployment_duration_in_minutes", int64(20))
	assertAttribute(t, stratAttrs, "growth_type", "EXPONENTIAL")

	// environment -> application edge, keyed by the application ARN the
	// application node publishes.
	envInApp := relationshipByType(t, envelopes, awscloud.RelationshipAppConfigEnvironmentInApplication)
	assertEdgeTarget(t, envInApp, awscloud.ResourceTypeAppConfigApplication, wantAppARN)
	if got, want := envInApp.Payload["source_resource_id"], wantEnvARN; got != want {
		t.Fatalf("env->app source_resource_id = %#v, want %q", got, want)
	}

	// profile -> application edge.
	profInApp := relationshipByType(t, envelopes, awscloud.RelationshipAppConfigProfileInApplication)
	assertEdgeTarget(t, profInApp, awscloud.ResourceTypeAppConfigApplication, wantAppARN)
	if got, want := profInApp.Payload["source_resource_id"], wantProfileARN; got != want {
		t.Fatalf("profile->app source_resource_id = %#v, want %q", got, want)
	}

	// environment -> CloudWatch alarm edge, keyed by the alarm ARN the
	// CloudWatch scanner publishes.
	envAlarm := relationshipByType(t, envelopes, awscloud.RelationshipAppConfigEnvironmentMonitorsAlarm)
	assertEdgeTarget(t, envAlarm, awscloud.ResourceTypeCloudWatchAlarm, testAlarmARN)
	if got, want := envAlarm.Payload["target_arn"], testAlarmARN; got != want {
		t.Fatalf("env->alarm target_arn = %#v, want %q", got, want)
	}

	// environment -> IAM role edge (monitor alarm role).
	envRole := relationshipByType(t, envelopes, awscloud.RelationshipAppConfigEnvironmentUsesMonitorRole)
	assertEdgeTarget(t, envRole, awscloud.ResourceTypeIAMRole, testRoleARN)

	// No configuration content / values anywhere in the resource payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"content", "configuration", "configuration_content", "value", "values",
			"flag_values", "feature_flags", "freeform", "hosted_configuration",
			"configuration_version",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; AppConfig scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerSynthesizesGovCloudARNs(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	client := fakeClient{snapshot: Snapshot{Applications: []Application{{
		ID:   testAppID,
		Name: "checkout",
		Environments: []Environment{{
			ID:            testEnvID,
			ApplicationID: testAppID,
			Name:          "prod",
		}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	environment := resourceByType(t, envelopes, awscloud.ResourceTypeAppConfigEnvironment)
	wantARN := "arn:aws-us-gov:appconfig:us-gov-west-1:123456789012:application/" + testAppID + "/environment/" + testEnvID
	if got := environment.Payload["resource_id"]; got != wantARN {
		t.Fatalf("GovCloud environment resource_id = %#v, want %q", got, wantARN)
	}
}

func TestScannerSynthesizesChinaARNs(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "cn-north-1"
	client := fakeClient{snapshot: Snapshot{Applications: []Application{{
		ID:   testAppID,
		Name: "checkout",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	application := resourceByType(t, envelopes, awscloud.ResourceTypeAppConfigApplication)
	wantARN := "arn:aws-cn:appconfig:cn-north-1:123456789012:application/" + testAppID
	if got := application.Payload["arn"]; got != wantARN {
		t.Fatalf("China application arn = %#v, want %q", got, wantARN)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Applications: []Application{{
		ID:   testAppID,
		Name: "checkout",
		// No environments, no profiles: no membership or monitor edges.
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted: %#v", envelope.Payload)
		}
	}
}

func TestScannerOmitsMonitorRoleEdgeForNonARNRole(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Applications: []Application{{
		ID:   testAppID,
		Name: "checkout",
		Environments: []Environment{{
			ID:            testEnvID,
			ApplicationID: testAppID,
			Name:          "prod",
			Monitors: []Monitor{{
				AlarmARN:     testAlarmARN,
				AlarmRoleARN: "not-an-arn",
			}},
		}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	// The alarm edge is still emitted; the role edge is skipped, not dangled.
	relationshipByType(t, envelopes, awscloud.RelationshipAppConfigEnvironmentMonitorsAlarm)
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == awscloud.RelationshipAppConfigEnvironmentUsesMonitorRole {
			t.Fatalf("monitor-role edge emitted for non-ARN role identifier")
		}
	}
}

func TestScannerEmitsDeploymentStrategyWithoutApplication(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		DeploymentStrategies: []DeploymentStrategy{{
			ID:   testStrategyID,
			Name: "AllAtOnce",
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	strategy := resourceByType(t, envelopes, awscloud.ResourceTypeAppConfigDeploymentStrategy)
	if got, want := strategy.Payload["resource_id"], wantStrategyARN; got != want {
		t.Fatalf("strategy resource_id = %#v, want %q", got, want)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	appARN := applicationARN(boundary, testAppID)
	envARN := environmentARN(boundary, testAppID, testEnvID)
	profARN := profileARN(boundary, testAppID, testProfileID)
	monitor := Monitor{AlarmARN: testAlarmARN, AlarmRoleARN: testRoleARN}
	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		environmentInApplicationRelationship(boundary, envARN, appARN),
		profileInApplicationRelationship(boundary, profARN, appARN),
		environmentMonitorsAlarmRelationship(boundary, envARN, monitor),
		environmentMonitorRoleRelationship(boundary, envARN, monitor),
	} {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
	}
	relguard.AssertObservations(t, observations...)
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
		Applications: []Application{{ID: testAppID, Name: "checkout"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "AppConfig ListEnvironments throttled after SDK retries; environment metadata omitted for this scan",
			SourceRecordID: "appconfig_environments_throttled",
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
		ServiceKind:         awscloud.ServiceAppConfig,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:appconfig:1",
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
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
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

func assertEdgeTarget(t *testing.T, envelope facts.Envelope, targetType, targetResourceID string) {
	t.Helper()
	if got := envelope.Payload["target_type"]; got != targetType {
		t.Fatalf("target_type = %#v, want %q", got, targetType)
	}
	if got := envelope.Payload["target_resource_id"]; got != targetResourceID {
		t.Fatalf("target_resource_id = %#v, want %q", got, targetResourceID)
	}
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
	if !valuesEqual(got, want) {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}

func valuesEqual(got any, want any) bool {
	switch want := want.(type) {
	case []string:
		gotSlice, ok := got.([]string)
		if !ok || len(gotSlice) != len(want) {
			return false
		}
		for i := range want {
			if gotSlice[i] != want[i] {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
