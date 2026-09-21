// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resourcegroups

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
)

func TestScannerEmitsGroupResourceAndMembershipEdges(t *testing.T) {
	groupARN := "arn:aws:resource-groups:us-east-1:123456789012:group/web-tier"
	bucketARN := "arn:aws:s3:::example-assets"
	instanceARN := "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc1234"

	client := fakeClient{groups: []Group{{
		ARN:          groupARN,
		Name:         "web-tier",
		Description:  "Web tier resources",
		QueryType:    "TAG_FILTERS_1_0",
		CreationTime: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Members: []ResourceMember{
			{ARN: bucketARN, ResourceType: "AWS::S3::Bucket"},
			{ARN: instanceARN, ResourceType: "AWS::EC2::Instance"},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	group := resourceByType(t, envelopes, awscloud.ResourceTypeResourceGroupsGroup)
	if got, want := group.Payload["resource_id"], groupARN; got != want {
		t.Fatalf("group resource_id = %#v, want %q", got, want)
	}
	if got, want := group.Payload["name"], "web-tier"; got != want {
		t.Fatalf("group name = %#v, want %q", got, want)
	}
	attrs := attributesOf(t, group)
	if got, want := attrs["query_type"], "TAG_FILTERS_1_0"; got != want {
		t.Fatalf("group query_type = %#v, want %q", got, want)
	}
	if got, want := attrs["member_count"], 2; got != want {
		t.Fatalf("group member_count = %#v, want %d", got, want)
	}

	// S3 bucket is an ARN-keyed family: target_resource_id and target_arn are the
	// member ARN.
	s3Edge := relationshipTo(t, envelopes, awscloud.RelationshipResourceGroupsGroupContainsResource, bucketARN)
	if got, want := s3Edge.TargetType, awscloud.ResourceTypeS3Bucket; got != want {
		t.Fatalf("s3 member target_type = %q, want %q", got, want)
	}
	if got, want := s3Edge.TargetARN, bucketARN; got != want {
		t.Fatalf("s3 member target_arn = %q, want %q", got, want)
	}
	if got, want := s3Edge.SourceResourceID, groupARN; got != want {
		t.Fatalf("s3 member source_resource_id = %q, want %q", got, want)
	}

	// EC2 instance is a bare-id family: target_resource_id is i-..., target_arn
	// is empty so the edge is not mis-keyed as ARN-keyed.
	instanceEdge := relationshipTo(t, envelopes, awscloud.RelationshipResourceGroupsGroupContainsResource, "i-0abc1234")
	if got, want := instanceEdge.TargetType, "aws_ec2_instance"; got != want {
		t.Fatalf("ec2 member target_type = %q, want %q", got, want)
	}
	if instanceEdge.TargetARN != "" {
		t.Fatalf("ec2 member target_arn = %q, want empty (bare-id keyed target)", instanceEdge.TargetARN)
	}

	relguard.AssertObservations(t, relationships(envelopes)...)
}

// TestClassifyMemberCoversFamilies asserts the ARN->(target_type, target_resource_id,
// arn-keyed) classification for every member family the scanner recognizes,
// proving each published identity matches the target scanner's own resource_id
// shape (ARN-equality, bare id, or prefixed id).
func TestClassifyMemberCoversFamilies(t *testing.T) {
	cases := []struct {
		name       string
		arn        string
		wantType   string
		wantID     string
		wantARNKey bool
	}{
		{"s3 bucket", "arn:aws:s3:::example-assets", awscloud.ResourceTypeS3Bucket, "arn:aws:s3:::example-assets", true},
		{"lambda function", "arn:aws:lambda:us-east-1:123456789012:function:handler", awscloud.ResourceTypeLambdaFunction, "arn:aws:lambda:us-east-1:123456789012:function:handler", true},
		{"dynamodb table", "arn:aws:dynamodb:us-east-1:123456789012:table/users", awscloud.ResourceTypeDynamoDBTable, "arn:aws:dynamodb:us-east-1:123456789012:table/users", true},
		{"sqs queue", "arn:aws:sqs:us-east-1:123456789012:orders", awscloud.ResourceTypeSQSQueue, "arn:aws:sqs:us-east-1:123456789012:orders", true},
		{"sns topic", "arn:aws:sns:us-east-1:123456789012:alerts", awscloud.ResourceTypeSNSTopic, "arn:aws:sns:us-east-1:123456789012:alerts", true},
		{"kinesis stream", "arn:aws:kinesis:us-east-1:123456789012:stream/events", awscloud.ResourceTypeKinesisDataStream, "arn:aws:kinesis:us-east-1:123456789012:stream/events", true},
		{"rds instance", "arn:aws:rds:us-east-1:123456789012:db:prod-db", awscloud.ResourceTypeRDSDBInstance, "arn:aws:rds:us-east-1:123456789012:db:prod-db", true},
		{"rds cluster", "arn:aws:rds:us-east-1:123456789012:cluster:prod-cluster", awscloud.ResourceTypeRDSDBCluster, "arn:aws:rds:us-east-1:123456789012:cluster:prod-cluster", true},
		{"ecs cluster", "arn:aws:ecs:us-east-1:123456789012:cluster/main", awscloud.ResourceTypeECSCluster, "arn:aws:ecs:us-east-1:123456789012:cluster/main", true},
		{"ecs service", "arn:aws:ecs:us-east-1:123456789012:service/main/web", awscloud.ResourceTypeECSService, "arn:aws:ecs:us-east-1:123456789012:service/main/web", true},
		{"eks cluster", "arn:aws:eks:us-east-1:123456789012:cluster/prod", awscloud.ResourceTypeEKSCluster, "arn:aws:eks:us-east-1:123456789012:cluster/prod", true},
		{"elbv2 load balancer", "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/abc", awscloud.ResourceTypeELBv2LoadBalancer, "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/abc", true},
		{"secret", "arn:aws:secretsmanager:us-east-1:123456789012:secret:prod/db-AbCdEf", awscloud.ResourceTypeSecretsManagerSecret, "arn:aws:secretsmanager:us-east-1:123456789012:secret:prod/db-AbCdEf", true},
		{"cloudformation stack", "arn:aws:cloudformation:us-east-1:123456789012:stack/my-stack/guid", awscloud.ResourceTypeCloudFormationStack, "arn:aws:cloudformation:us-east-1:123456789012:stack/my-stack/guid", true},

		// Non-ARN-keyed families: published id must match the target scanner.
		{"kms key", "arn:aws:kms:us-east-1:123456789012:key/abcd1234-12ab-34cd-56ef-1234567890ab", awscloud.ResourceTypeKMSKey, "abcd1234-12ab-34cd-56ef-1234567890ab", false},
		{"route53 hosted zone", "arn:aws:route53:::hostedzone/Z123456ABC", awscloud.ResourceTypeRoute53HostedZone, "/hostedzone/Z123456ABC", false},
		{"ec2 instance", "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc1234", "aws_ec2_instance", "i-0abc1234", false},
		{"ec2 vpc", "arn:aws:ec2:us-east-1:123456789012:vpc/vpc-0abc1234", awscloud.ResourceTypeEC2VPC, "vpc-0abc1234", false},
		{"ec2 subnet", "arn:aws:ec2:us-east-1:123456789012:subnet/subnet-0abc1234", awscloud.ResourceTypeEC2Subnet, "subnet-0abc1234", false},
		{"ec2 security group", "arn:aws:ec2:us-east-1:123456789012:security-group/sg-0abc1234", awscloud.ResourceTypeEC2SecurityGroup, "sg-0abc1234", false},
		{"ec2 elastic ip", "arn:aws:ec2:us-east-1:123456789012:elastic-ip/eipalloc-0abc1234", awscloud.ResourceTypeVPCElasticIP, "eipalloc-0abc1234", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target, ok := classifyMember(ResourceMember{ARN: tc.arn})
			if !ok {
				t.Fatalf("classifyMember(%q) ok = false, want true", tc.arn)
			}
			if target.Type != tc.wantType {
				t.Errorf("target_type = %q, want %q", target.Type, tc.wantType)
			}
			if target.ResourceID != tc.wantID {
				t.Errorf("target_resource_id = %q, want %q", target.ResourceID, tc.wantID)
			}
			if target.ARNKeyed != tc.wantARNKey {
				t.Errorf("arn-keyed = %v, want %v", target.ARNKeyed, tc.wantARNKey)
			}
		})
	}
}

// TestClassifyMemberSkipsUnrecognizedFamily proves an unrecognized resource
// family is skipped (ok=false), not mapped to a generic or empty target type.
func TestClassifyMemberSkipsUnrecognizedFamily(t *testing.T) {
	unrecognized := []string{
		"arn:aws:imagebuilder:us-east-1:123456789012:image-pipeline/my-pipeline",
		"arn:aws:greengrass:us-east-1:123456789012:greengrass/definition/cores/abc",
		"arn:aws:ec2:us-east-1:123456789012:volume/vol-0abc1234",
		"arn:aws:ec2:us-east-1:123456789012:snapshot/snap-0abc1234",
		"not-an-arn",
		"arn:aws:s3",
		"",
	}
	for _, arn := range unrecognized {
		if _, ok := classifyMember(ResourceMember{ARN: arn}); ok {
			t.Errorf("classifyMember(%q) ok = true, want false (unrecognized family must skip)", arn)
		}
	}
}

func TestScannerSkipsUnrecognizedMembersWithoutDanglingEdges(t *testing.T) {
	groupARN := "arn:aws:resource-groups:us-east-1:123456789012:group/mixed"
	client := fakeClient{groups: []Group{{
		ARN:       groupARN,
		Name:      "mixed",
		QueryType: "TAG_FILTERS_1_0",
		Members: []ResourceMember{
			{ARN: "arn:aws:s3:::known-bucket", ResourceType: "AWS::S3::Bucket"},
			{ARN: "arn:aws:imagebuilder:us-east-1:123456789012:image/x/1.0.0/1", ResourceType: "AWS::ImageBuilder::Image"},
			{ARN: "arn:aws:ec2:us-east-1:123456789012:volume/vol-1", ResourceType: "AWS::EC2::Volume"},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	members := relationships(envelopes)
	if got, want := len(members), 1; got != want {
		t.Fatalf("membership edge count = %d, want %d (only the recognized S3 member)", got, want)
	}
	if got, want := members[0].TargetResourceID, "arn:aws:s3:::known-bucket"; got != want {
		t.Fatalf("only edge target_resource_id = %q, want %q", got, want)
	}
	// No edge may carry an empty target_type.
	relguard.AssertObservations(t, members...)
}

func TestScannerEmitsCloudFormationStackEdge(t *testing.T) {
	groupARN := "arn:aws:resource-groups:us-east-1:123456789012:group/cfn-app"
	stackARN := "arn:aws:cloudformation:us-east-1:123456789012:stack/my-app/abcd-guid"
	client := fakeClient{groups: []Group{{
		ARN:             groupARN,
		Name:            "cfn-app",
		QueryType:       "CLOUDFORMATION_STACK_1_0",
		StackIdentifier: stackARN,
		Members: []ResourceMember{
			{ARN: "arn:aws:s3:::cfn-bucket", ResourceType: "AWS::S3::Bucket"},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	stackEdge := relationshipTo(t, envelopes, awscloud.RelationshipResourceGroupsGroupBackedByStack, stackARN)
	if got, want := stackEdge.TargetType, awscloud.ResourceTypeCloudFormationStack; got != want {
		t.Fatalf("stack edge target_type = %q, want %q", got, want)
	}
	if got, want := stackEdge.TargetARN, stackARN; got != want {
		t.Fatalf("stack edge target_arn = %q, want %q", got, want)
	}
	relguard.AssertObservations(t, relationships(envelopes)...)
}

// TestStackEdgeSkippedForTagFilterGroup proves the group-to-stack edge is only
// emitted for CloudFormation-stack-backed groups.
func TestStackEdgeSkippedForTagFilterGroup(t *testing.T) {
	client := fakeClient{groups: []Group{{
		ARN:             "arn:aws:resource-groups:us-east-1:123456789012:group/tagged",
		Name:            "tagged",
		QueryType:       "TAG_FILTERS_1_0",
		StackIdentifier: "arn:aws:cloudformation:us-east-1:123456789012:stack/x/guid",
	}}}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, obs := range relationships(envelopes) {
		if obs.RelationshipType == awscloud.RelationshipResourceGroupsGroupBackedByStack {
			t.Fatalf("tag-filter group emitted a stack edge: %#v", obs)
		}
	}
}

// TestScannerIsPartitionAware proves the scanner keys edges off the
// API-reported ARN partition rather than a hardcoded commercial partition, so
// GovCloud and China members join their real nodes.
func TestScannerIsPartitionAware(t *testing.T) {
	groupARN := "arn:aws-us-gov:resource-groups:us-gov-west-1:123456789012:group/gov-tier"
	govBucketARN := "arn:aws-us-gov:s3:::gov-assets"
	client := fakeClient{groups: []Group{{
		ARN:       groupARN,
		Name:      "gov-tier",
		QueryType: "TAG_FILTERS_1_0",
		Members:   []ResourceMember{{ARN: govBucketARN, ResourceType: "AWS::S3::Bucket"}},
	}}}
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edge := relationshipTo(t, envelopes, awscloud.RelationshipResourceGroupsGroupContainsResource, govBucketARN)
	if !strings.HasPrefix(edge.TargetARN, "arn:aws-us-gov:") {
		t.Fatalf("gov member target_arn = %q, want aws-us-gov partition preserved", edge.TargetARN)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = "s3"
	if _, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary); err == nil {
		t.Fatalf("Scan() error = nil, want service_kind mismatch error")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	if _, err := (Scanner{}).Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

// TestClientInterfaceExcludesMutationAndQueryBodyAPIs is the metadata-only
// exclusion proof: the Client interface is the only path the scanner has to AWS
// Resource Groups, so asserting its exact method set proves mutation and
// query-body reads are unreachable from this code path.
func TestClientInterfaceExcludesMutationAndQueryBodyAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	want := map[string]bool{"ListGroups": true}
	have := map[string]bool{}
	for i := 0; i < clientType.NumMethod(); i++ {
		have[clientType.Method(i).Name] = true
	}
	for name := range want {
		if !have[name] {
			t.Errorf("Client interface missing required method %q", name)
		}
	}
	for name := range have {
		if !want[name] {
			t.Errorf("Client interface exposes unexpected method %q; metadata-only contract violated", name)
		}
	}
	forbidden := []string{
		"Create", "Update", "Delete", "Put", "Group", "Ungroup",
		"Tag", "Untag", "Associate", "Disassociate",
	}
	for name := range have {
		if want[name] {
			continue
		}
		for _, bad := range forbidden {
			if strings.Contains(name, bad) {
				t.Errorf("Client method %q contains forbidden substring %q", name, bad)
			}
		}
	}
}

// TestScannerNeverPersistsQueryBody proves the group resource fact records the
// query type only and never the resource-query body, which can carry tag keys
// and values.
func TestScannerNeverPersistsQueryBody(t *testing.T) {
	client := fakeClient{groups: []Group{{
		ARN:       "arn:aws:resource-groups:us-east-1:123456789012:group/g",
		Name:      "g",
		QueryType: "TAG_FILTERS_1_0",
	}}}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	attrs := attributesOf(t, resourceByType(t, envelopes, awscloud.ResourceTypeResourceGroupsGroup))
	for _, forbidden := range []string{"query", "query_body", "resource_query", "tag_filters", "tags", "search_query"} {
		if _, exists := attrs[forbidden]; exists {
			t.Fatalf("group attribute %q persisted; scanner must record query type only", forbidden)
		}
	}
}
