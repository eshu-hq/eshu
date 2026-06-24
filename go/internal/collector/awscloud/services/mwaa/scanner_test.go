// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mwaa

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsMWAAEnvironmentResourceAndRelationships(t *testing.T) {
	environmentName := "analytics-airflow"
	environmentARN := "arn:aws:airflow:us-east-1:123456789012:environment/analytics-airflow"
	executionRoleARN := "arn:aws:iam::123456789012:role/mwaa-execution"
	kmsKeyARN := "arn:aws:kms:us-east-1:123456789012:key/abcd-1234"
	bucketARN := "arn:aws:s3:::analytics-airflow-dags"
	logGroupBase := "arn:aws:logs:us-east-1:123456789012:log-group:airflow-analytics-DAGProcessing"

	client := fakeClient{environments: []Environment{{
		Name:                environmentName,
		ARN:                 environmentARN,
		Status:              "AVAILABLE",
		AirflowVersion:      "2.10.1",
		WebserverAccessMode: "PUBLIC_ONLY",
		EnvironmentClass:    "mw1.small",
		EndpointManagement:  "SERVICE",
		Schedulers:          2,
		MinWorkers:          1,
		MaxWorkers:          10,
		MinWebservers:       2,
		MaxWebservers:       2,
		CreatedAt:           time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC),
		SourceBucketARN:     bucketARN,
		ExecutionRoleARN:    executionRoleARN,
		ServiceRoleARN:      "arn:aws:iam::123456789012:role/aws-service-role/airflow.amazonaws.com/AWSServiceRoleForAmazonMWAA",
		KMSKey:              kmsKeyARN,
		SubnetIDs:           []string{"subnet-aaa", "subnet-bbb"},
		SecurityGroupIDs:    []string{"sg-111"},
		LogGroups: []LogGroup{{
			Module:   "DagProcessingLogs",
			ARN:      logGroupBase + ":*",
			Enabled:  true,
			LogLevel: "INFO",
		}},
		Tags: map[string]string{"team": "data"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	environment := resourceByType(t, envelopes, awscloud.ResourceTypeMWAAEnvironment)
	if got, want := environment.Payload["resource_id"], environmentARN; got != want {
		t.Fatalf("environment resource_id = %#v, want %q", got, want)
	}
	if got, want := environment.Payload["name"], environmentName; got != want {
		t.Fatalf("environment name = %#v, want %q", got, want)
	}
	if got, want := environment.Payload["state"], "AVAILABLE"; got != want {
		t.Fatalf("environment state = %#v, want %q", got, want)
	}
	attributes := attributesOf(t, environment)
	if got, want := attributes["airflow_version"], "2.10.1"; got != want {
		t.Fatalf("environment airflow_version = %#v, want %q", got, want)
	}
	if got, want := attributes["webserver_access_mode"], "PUBLIC_ONLY"; got != want {
		t.Fatalf("environment webserver_access_mode = %#v, want %q", got, want)
	}

	// HARD CONTRACT: no Airflow configuration option values, connection strings,
	// or secrets ever appear in the environment attributes.
	for _, forbidden := range []string{
		"airflow_configuration_options",
		"airflow_configuration",
		"configuration_options",
		"connection_string",
		"connection_strings",
		"celery_executor_queue",
		"database_vpc_endpoint_service",
		"cli_token",
		"web_login_token",
		"webserver_url",
	} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("environment attribute %q persisted; MWAA scanner must never emit Airflow config values or secrets", forbidden)
		}
	}

	s3Edge := relationshipByType(t, envelopes, awscloud.RelationshipMWAAEnvironmentUsesS3Bucket)
	assertEdge(t, s3Edge, environmentARN, bucketARN, awscloud.ResourceTypeS3Bucket)
	if got, want := s3Edge.Payload["target_arn"], bucketARN; got != want {
		t.Fatalf("env->s3 target_arn = %#v, want %q", got, want)
	}

	iamEdge := relationshipByType(t, envelopes, awscloud.RelationshipMWAAEnvironmentUsesIAMRole)
	assertEdge(t, iamEdge, environmentARN, executionRoleARN, awscloud.ResourceTypeIAMRole)
	if got, want := iamEdge.Payload["target_arn"], executionRoleARN; got != want {
		t.Fatalf("env->iam target_arn = %#v, want %q", got, want)
	}

	kmsEdge := relationshipByType(t, envelopes, awscloud.RelationshipMWAAEnvironmentUsesKMSKey)
	assertEdge(t, kmsEdge, environmentARN, kmsKeyARN, awscloud.ResourceTypeKMSKey)

	subnetEdges := relationshipsByType(envelopes, awscloud.RelationshipMWAAEnvironmentUsesSubnet)
	if got := len(subnetEdges); got != 2 {
		t.Fatalf("env->subnet edge count = %d, want 2", got)
	}
	assertEdge(t, subnetEdges[0], environmentARN, "subnet-aaa", awscloud.ResourceTypeEC2Subnet)
	assertEdge(t, subnetEdges[1], environmentARN, "subnet-bbb", awscloud.ResourceTypeEC2Subnet)

	sgEdge := relationshipByType(t, envelopes, awscloud.RelationshipMWAAEnvironmentUsesSecurityGroup)
	assertEdge(t, sgEdge, environmentARN, "sg-111", awscloud.ResourceTypeEC2SecurityGroup)

	logEdge := relationshipByType(t, envelopes, awscloud.RelationshipMWAAEnvironmentLogsToCloudWatchLogGroup)
	// The trailing ":*" wildcard suffix MWAA returns must be trimmed so the edge
	// joins the cloudwatchlogs scanner's published non-wildcard ARN resource_id.
	assertEdge(t, logEdge, environmentARN, logGroupBase, awscloud.ResourceTypeCloudWatchLogsLogGroup)
	if got, want := logEdge.Payload["target_arn"], logGroupBase; got != want {
		t.Fatalf("env->log-group target_arn = %#v, want %q (wildcard must be trimmed)", got, want)
	}
	logAttributes := attributesOf(t, logEdge)
	if got, want := logAttributes["log_module"], "DagProcessingLogs"; got != want {
		t.Fatalf("env->log-group log_module = %#v, want %q", got, want)
	}

	relguard.AssertObservations(t, environmentRelationships(testBoundary(), client.environments[0])...)
}

func TestScannerSynthesizesS3BucketARNFromBareNameAcrossPartitions(t *testing.T) {
	cases := []struct {
		name    string
		region  string
		wantARN string
	}{
		{name: "commercial", region: "us-east-1", wantARN: "arn:aws:s3:::dag-bucket"},
		{name: "govcloud", region: "us-gov-west-1", wantARN: "arn:aws-us-gov:s3:::dag-bucket"},
		{name: "china", region: "cn-north-1", wantARN: "arn:aws-cn:s3:::dag-bucket"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			boundary := testBoundary()
			boundary.Region = tc.region
			client := fakeClient{environments: []Environment{{
				Name:            "env",
				ARN:             "arn:" + partitionForRegion(tc.region) + ":airflow:" + tc.region + ":123456789012:environment/env",
				SourceBucketARN: "dag-bucket",
			}}}

			envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
			if err != nil {
				t.Fatalf("Scan() error = %v, want nil", err)
			}
			edge := relationshipByType(t, envelopes, awscloud.RelationshipMWAAEnvironmentUsesS3Bucket)
			if got := edge.Payload["target_resource_id"]; got != tc.wantARN {
				t.Fatalf("env->s3 target_resource_id = %#v, want %q", got, tc.wantARN)
			}
			if got := edge.Payload["target_arn"]; got != tc.wantARN {
				t.Fatalf("env->s3 target_arn = %#v, want %q", got, tc.wantARN)
			}
		})
	}
}

func TestS3BucketARNBlankRegionFallsBackToCommercialPartition(t *testing.T) {
	// A blank boundary region must fall back to the commercial partition so the
	// synthesized bucket ARN never carries an empty partition segment.
	boundary := testBoundary()
	boundary.Region = ""
	if got, want := s3BucketARN(boundary, "dag-bucket"), "arn:aws:s3:::dag-bucket"; got != want {
		t.Fatalf("s3BucketARN(blank region) = %q, want %q", got, want)
	}
}

func TestS3BucketARNPassesThroughReportedARNUnchanged(t *testing.T) {
	// When MWAA reports a full bucket ARN it is used verbatim so it inherits its
	// own partition rather than being re-synthesized from the boundary.
	boundary := testBoundary()
	boundary.Region = "cn-north-1"
	if got, want := s3BucketARN(boundary, "arn:aws-cn:s3:::reported-bucket"), "arn:aws-cn:s3:::reported-bucket"; got != want {
		t.Fatalf("s3BucketARN(reported arn) = %q, want %q", got, want)
	}
}

func TestScannerSourcesEdgesOnEnvironmentResourceID(t *testing.T) {
	// When the environment has no ARN, the resource publishes the name as its
	// resource_id, and every edge must be sourced on that same name.
	client := fakeClient{environments: []Environment{{
		Name:             "no-arn-env",
		SourceBucketARN:  "arn:aws:s3:::b",
		ExecutionRoleARN: "arn:aws:iam::123456789012:role/r",
		SubnetIDs:        []string{"subnet-x"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	resource := resourceByType(t, envelopes, awscloud.ResourceTypeMWAAEnvironment)
	if got, want := resource.Payload["resource_id"], "no-arn-env"; got != want {
		t.Fatalf("environment resource_id = %#v, want %q", got, want)
	}
	for _, relationshipType := range []string{
		awscloud.RelationshipMWAAEnvironmentUsesS3Bucket,
		awscloud.RelationshipMWAAEnvironmentUsesIAMRole,
		awscloud.RelationshipMWAAEnvironmentUsesSubnet,
	} {
		edge := relationshipByType(t, envelopes, relationshipType)
		if got, want := edge.Payload["source_resource_id"], "no-arn-env"; got != want {
			t.Fatalf("%s source_resource_id = %#v, want %q", relationshipType, got, want)
		}
	}
}

func TestScannerOmitsRelationshipsWhenTargetsMissing(t *testing.T) {
	client := fakeClient{environments: []Environment{{
		Name:             "bare-env",
		ARN:              "arn:aws:airflow:us-east-1:123456789012:environment/bare-env",
		ExecutionRoleARN: "not-an-arn",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, relationshipType := range []string{
		awscloud.RelationshipMWAAEnvironmentUsesS3Bucket,
		awscloud.RelationshipMWAAEnvironmentUsesIAMRole,
		awscloud.RelationshipMWAAEnvironmentUsesKMSKey,
		awscloud.RelationshipMWAAEnvironmentUsesSubnet,
		awscloud.RelationshipMWAAEnvironmentUsesSecurityGroup,
		awscloud.RelationshipMWAAEnvironmentLogsToCloudWatchLogGroup,
	} {
		if got := len(relationshipsByType(envelopes, relationshipType)); got != 0 {
			t.Fatalf("%s edge count = %d, want 0 when target is missing or non-ARN", relationshipType, got)
		}
	}
}

func TestScannerTrimsLogGroupWildcardAndDeduplicates(t *testing.T) {
	logGroup := "arn:aws:logs:us-east-1:123456789012:log-group:airflow-env-Scheduler"
	client := fakeClient{environments: []Environment{{
		Name: "env",
		ARN:  "arn:aws:airflow:us-east-1:123456789012:environment/env",
		LogGroups: []LogGroup{
			{Module: "SchedulerLogs", ARN: logGroup + ":*", Enabled: true},
			{Module: "DuplicateForSameGroup", ARN: logGroup, Enabled: true},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edges := relationshipsByType(envelopes, awscloud.RelationshipMWAAEnvironmentLogsToCloudWatchLogGroup)
	if got := len(edges); got != 1 {
		t.Fatalf("env->log-group edge count = %d, want 1 after wildcard-trim dedupe", got)
	}
	if got, want := edges[0].Payload["target_resource_id"], logGroup; got != want {
		t.Fatalf("env->log-group target_resource_id = %#v, want %q", got, want)
	}
}

func TestScannerCanonicalizesPaddedServiceKind(t *testing.T) {
	// A caller may pass a service_kind padded with whitespace. The switch only
	// trims for the comparison, so the canonical value must be written back to
	// the boundary; otherwise the padded string leaks into every emitted fact's
	// service_kind and breaks joins/filters that expect the canonical "mwaa".
	boundary := testBoundary()
	boundary.ServiceKind = "  " + awscloud.ServiceMWAA + "  "
	client := fakeClient{environments: []Environment{{
		Name:            "padded-env",
		ARN:             "arn:aws:airflow:us-east-1:123456789012:environment/padded-env",
		SourceBucketARN: "arn:aws:s3:::padded-dags",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, envelope := range envelopes {
		if got, want := envelope.Payload["service_kind"], awscloud.ServiceMWAA; got != want {
			t.Fatalf("envelope service_kind = %#v, want %q (padded service_kind must be canonicalized)", got, want)
		}
	}
}

func TestScannerSkipsDisabledLogModuleRelationships(t *testing.T) {
	// AWS reports a CloudWatch log group ARN even for disabled Airflow log
	// modules. A disabled module does not publish logs, so it must not emit an
	// env->log-group edge that would create misleading dependency evidence.
	enabledLogGroup := "arn:aws:logs:us-east-1:123456789012:log-group:airflow-env-Scheduler"
	disabledLogGroup := "arn:aws:logs:us-east-1:123456789012:log-group:airflow-env-Worker"
	client := fakeClient{environments: []Environment{{
		Name: "env",
		ARN:  "arn:aws:airflow:us-east-1:123456789012:environment/env",
		LogGroups: []LogGroup{
			{Module: "SchedulerLogs", ARN: enabledLogGroup + ":*", Enabled: true},
			{Module: "WorkerLogs", ARN: disabledLogGroup + ":*", Enabled: false},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edges := relationshipsByType(envelopes, awscloud.RelationshipMWAAEnvironmentLogsToCloudWatchLogGroup)
	if got := len(edges); got != 1 {
		t.Fatalf("env->log-group edge count = %d, want 1 (disabled module must not emit an edge)", got)
	}
	if got, want := edges[0].Payload["target_resource_id"], enabledLogGroup; got != want {
		t.Fatalf("env->log-group target_resource_id = %#v, want %q (only the enabled module)", got, want)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceSNS

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

func partitionForRegion(region string) string {
	return awscloud.PartitionForRegion(region)
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceMWAA,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:mwaa:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 20, 16, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	environments []Environment
}

func (c fakeClient) ListEnvironments(context.Context) ([]Environment, error) {
	return c.environments, nil
}

func assertEdge(t *testing.T, envelope facts.Envelope, sourceID, targetID, targetType string) {
	t.Helper()
	if got := envelope.Payload["source_resource_id"]; got != sourceID {
		t.Fatalf("edge source_resource_id = %#v, want %q", got, sourceID)
	}
	if got := envelope.Payload["target_resource_id"]; got != targetID {
		t.Fatalf("edge target_resource_id = %#v, want %q", got, targetID)
	}
	if got := envelope.Payload["target_type"]; got != targetType {
		t.Fatalf("edge target_type = %#v, want %q", got, targetType)
	}
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
	t.Fatalf("missing resource_type %q in %d envelopes", resourceType, len(envelopes))
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
	t.Fatalf("missing relationship_type %q in %d envelopes", relationshipType, len(envelopes))
	return facts.Envelope{}
}

func relationshipsByType(envelopes []facts.Envelope, relationshipType string) []facts.Envelope {
	var matches []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			matches = append(matches, envelope)
		}
	}
	return matches
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
