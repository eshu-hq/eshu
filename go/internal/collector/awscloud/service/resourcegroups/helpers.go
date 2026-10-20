// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package resourcegroups

import (
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// queryTypeCloudFormationStack is the AWS Resource Groups query type that marks
// a group whose membership is derived from a CloudFormation stack. The scanner
// uses it to decide whether to emit a group-to-stack edge in addition to (or
// instead of) per-member edges.
const queryTypeCloudFormationStack = "CLOUDFORMATION_STACK_1_0"

// arnFields is the parsed view of an AWS ARN split on its colon separators:
// arn:partition:service:region:account:resource. Resource is the remainder
// after the fifth colon and may itself contain "/" or ":" separators, which the
// classifier inspects per service.
type arnFields struct {
	Partition string
	Service   string
	Region    string
	Account   string
	Resource  string
}

// parseARN splits an AWS ARN into its component fields. It returns ok=false when
// the value is not an arn:-prefixed string with at least the six leading
// segments, so the caller can skip a malformed identifier rather than emit a
// dangling edge. The resource segment keeps any internal "/" or ":" because the
// resource-type and resource-id split differs per service.
func parseARN(value string) (arnFields, bool) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "arn:") {
		return arnFields{}, false
	}
	// SplitN with 6 keeps the resource (which can contain colons) intact.
	parts := strings.SplitN(trimmed, ":", 6)
	if len(parts) < 6 {
		return arnFields{}, false
	}
	fields := arnFields{
		Partition: parts[1],
		Service:   parts[2],
		Region:    parts[3],
		Account:   parts[4],
		Resource:  parts[5],
	}
	if fields.Partition == "" || fields.Service == "" {
		return arnFields{}, false
	}
	return fields, true
}

// resourceTypeAndID splits an ARN resource segment into its leading type token
// and the remaining identifier. AWS uses either "type/id" or "type:id" for the
// segment; the function splits on the first "/" or ":" it sees, whichever comes
// first, so it handles both forms. When the segment has no separator the whole
// segment is the id and the type token is empty (the bare-resource form, for
// example an S3 bucket name).
func resourceTypeAndID(resource string) (resourceType, id string) {
	slash := strings.IndexByte(resource, '/')
	colon := strings.IndexByte(resource, ':')
	switch {
	case slash < 0 && colon < 0:
		return "", resource
	case slash >= 0 && (colon < 0 || slash < colon):
		return resource[:slash], resource[slash+1:]
	default:
		return resource[:colon], resource[colon+1:]
	}
}

// memberTarget is the typed join target the classifier resolves for one member
// ARN. ResourceID is the identity the target family's scanner publishes:
// ARNEquality targets carry the full member ARN and set ARN=true so the edge is
// ARN-keyed; bare-id and prefixed-id targets carry the published id form and
// leave ARN=false so the relationship is not falsely marked ARN-keyed.
type memberTarget struct {
	// Type is a declared awscloud.ResourceType* value (or an allowlisted
	// forward-reference target_type) naming the member's resource family.
	Type string
	// ResourceID is the identity the target family's scanner publishes for this
	// member, used as the relationship target_resource_id.
	ResourceID string
	// ARNKeyed reports whether the target family is keyed by ARN-equality. When
	// true the relationship sets target_arn to the member ARN; when false the
	// relationship leaves target_arn empty so the bare/prefixed id is not
	// misread as an ARN-keyed join.
	ARNKeyed bool
}

// classifyMember maps a Resource Groups member ARN to its resource family and
// the identity that family's scanner publishes. It parses the ARN's colon
// fields exactly (never substring-matches the raw ARN) and returns ok=false for
// any family Eshu does not recognize, so the caller SKIPS the member rather than
// emitting an edge with an empty or guessed target type.
//
// The published identity must match the target scanner exactly or the edge
// dangles: ARN-keyed families (S3, Lambda, DynamoDB, SQS, SNS, Kinesis, RDS,
// ECS, EKS, ELBv2, Secrets Manager, CloudFormation stack) publish the full ARN;
// EC2 instance/VPC/subnet/security-group/network-interface and Elastic IP
// publish the bare id (i-..., vpc-..., eipalloc-..., etc.); KMS keys publish the
// bare key id; Route 53 hosted zones publish the "/hostedzone/<id>" prefixed id.
func classifyMember(member ResourceMember) (memberTarget, bool) {
	fields, ok := parseARN(member.ARN)
	if !ok {
		return memberTarget{}, false
	}
	resourceType, id := resourceTypeAndID(fields.Resource)
	arn := strings.TrimSpace(member.ARN)

	switch fields.Service {
	case "s3":
		// arn:p:s3:::bucket-name. The s3 scanner keys the bucket by its ARN.
		return arnTarget(awscloud.ResourceTypeS3Bucket, arn), true
	case "lambda":
		// arn:p:lambda:region:acct:function:name. The lambda scanner keys the
		// function by its ARN.
		if resourceType == "function" {
			return arnTarget(awscloud.ResourceTypeLambdaFunction, arn), true
		}
	case "dynamodb":
		if resourceType == "table" {
			return arnTarget(awscloud.ResourceTypeDynamoDBTable, arn), true
		}
	case "sqs":
		// SQS queue ARNs have no resource-type token; the resource segment is the
		// queue name. The sqs scanner keys the queue by its ARN.
		return arnTarget(awscloud.ResourceTypeSQSQueue, arn), true
	case "sns":
		// SNS topic ARNs have no resource-type token; the resource segment is the
		// topic name. The sns scanner keys the topic by its ARN.
		return arnTarget(awscloud.ResourceTypeSNSTopic, arn), true
	case "kinesis":
		if resourceType == "stream" {
			return arnTarget(awscloud.ResourceTypeKinesisDataStream, arn), true
		}
	case "rds":
		switch resourceType {
		case "cluster":
			return arnTarget(awscloud.ResourceTypeRDSDBCluster, arn), true
		case "db":
			return arnTarget(awscloud.ResourceTypeRDSDBInstance, arn), true
		}
	case "ecs":
		switch resourceType {
		case "cluster":
			return arnTarget(awscloud.ResourceTypeECSCluster, arn), true
		case "service":
			return arnTarget(awscloud.ResourceTypeECSService, arn), true
		case "task":
			return arnTarget(awscloud.ResourceTypeECSTask, arn), true
		}
	case "eks":
		if resourceType == "cluster" {
			return arnTarget(awscloud.ResourceTypeEKSCluster, arn), true
		}
	case "elasticloadbalancing":
		// arn:p:elasticloadbalancing:region:acct:loadbalancer/app/name/id. The
		// elbv2 scanner keys the load balancer by its ARN.
		if resourceType == "loadbalancer" {
			return arnTarget(awscloud.ResourceTypeELBv2LoadBalancer, arn), true
		}
	case "secretsmanager":
		if resourceType == "secret" {
			return arnTarget(awscloud.ResourceTypeSecretsManagerSecret, arn), true
		}
	case "cloudformation":
		// arn:p:cloudformation:region:acct:stack/name/guid. The cloudformation
		// scanner keys the stack by its StackId, which is this ARN.
		if resourceType == "stack" {
			return arnTarget(awscloud.ResourceTypeCloudFormationStack, arn), true
		}
	case "kms":
		// arn:p:kms:region:acct:key/<key-id>. The kms scanner keys the key by its
		// bare key id, NOT the ARN, so this target is not ARN-keyed.
		if resourceType == "key" && id != "" {
			return bareTarget(awscloud.ResourceTypeKMSKey, id), true
		}
	case "route53":
		// arn:p:route53:::hostedzone/<id>. The route53 scanner keys the hosted
		// zone by the "/hostedzone/<id>" prefixed id, so reconstruct that form.
		if resourceType == "hostedzone" && id != "" {
			return bareTarget(awscloud.ResourceTypeRoute53HostedZone, "/hostedzone/"+id), true
		}
	case "ec2":
		return classifyEC2Member(resourceType, id)
	}
	return memberTarget{}, false
}

// classifyEC2Member resolves the EC2-family resource types Resource Groups can
// report as members. EC2 vpc/subnet/security-group/network-interface/instance
// and Elastic IP are all keyed by their bare id (vpc-..., subnet-..., sg-...,
// eni-..., i-..., eipalloc-...) by their owning scanner, so none of these
// targets is ARN-keyed. Unrecognized EC2 sub-types return ok=false so the caller
// skips them.
func classifyEC2Member(resourceType, id string) (memberTarget, bool) {
	if id == "" {
		return memberTarget{}, false
	}
	switch resourceType {
	case "instance":
		// aws_ec2_instance is a documented forward reference: it is keyed by the
		// bare instance id (see relguard.KnownTargetTypeAllowlist).
		return bareTarget("aws_ec2_instance", id), true
	case "vpc":
		return bareTarget(awscloud.ResourceTypeEC2VPC, id), true
	case "subnet":
		return bareTarget(awscloud.ResourceTypeEC2Subnet, id), true
	case "security-group":
		return bareTarget(awscloud.ResourceTypeEC2SecurityGroup, id), true
	case "network-interface":
		return bareTarget(awscloud.ResourceTypeEC2NetworkInterface, id), true
	case "launch-template":
		return bareTarget(awscloud.ResourceTypeEC2LaunchTemplate, id), true
	case "elastic-ip":
		return bareTarget(awscloud.ResourceTypeVPCElasticIP, id), true
	default:
		return memberTarget{}, false
	}
}

// arnTarget builds an ARN-keyed member target: the relationship target_resource_id
// and target_arn are both the member ARN.
func arnTarget(targetType, arn string) memberTarget {
	return memberTarget{Type: targetType, ResourceID: arn, ARNKeyed: true}
}

// bareTarget builds a non-ARN-keyed member target whose target_resource_id is
// the bare or prefixed id the target scanner publishes; target_arn is left
// empty so the edge is not falsely marked ARN-keyed.
func bareTarget(targetType, id string) memberTarget {
	return memberTarget{Type: targetType, ResourceID: id, ARNKeyed: false}
}

// firstNonEmpty returns the first trimmed-non-empty value, or "".
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// timeOrNil returns the UTC time for attribute maps, or nil when the time is
// the zero value so the attribute is absent rather than a 0001-01-01 marker.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
