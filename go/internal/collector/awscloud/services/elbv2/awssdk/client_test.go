package awssdk

import (
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awselbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

func TestMapLoadBalancerPreservesDNSAndTags(t *testing.T) {
	createdAt := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	loadBalancer := mapLoadBalancer(awselbv2types.LoadBalancer{
		AvailabilityZones: []awselbv2types.AvailabilityZone{{
			ZoneName: aws.String("us-east-1a"),
			SubnetId: aws.String("subnet-1"),
		}},
		CanonicalHostedZoneId: aws.String("Z35SXDOTRQ7X7K"),
		CreatedTime:           aws.Time(createdAt),
		DNSName:               aws.String("api-123.us-east-1.elb.amazonaws.com"),
		IpAddressType:         awselbv2types.IpAddressTypeIpv4,
		LoadBalancerArn:       aws.String("arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/api/abc"),
		LoadBalancerName:      aws.String("api"),
		Scheme:                awselbv2types.LoadBalancerSchemeEnumInternetFacing,
		SecurityGroups:        []string{"sg-1"},
		State: &awselbv2types.LoadBalancerState{
			Code: awselbv2types.LoadBalancerStateEnumActive,
		},
		Type:  awselbv2types.LoadBalancerTypeEnumApplication,
		VpcId: aws.String("vpc-123"),
	}, map[string]string{"service": "api"})

	if loadBalancer.DNSName != "api-123.us-east-1.elb.amazonaws.com" {
		t.Fatalf("DNSName = %q", loadBalancer.DNSName)
	}
	if loadBalancer.Tags["service"] != "api" {
		t.Fatalf("tag service = %q, want api", loadBalancer.Tags["service"])
	}
	if loadBalancer.AvailabilityZones[0].SubnetID != "subnet-1" {
		t.Fatalf("subnet = %q", loadBalancer.AvailabilityZones[0].SubnetID)
	}
}

func TestMapRulePreservesTypedConditionsAndForwardTargets(t *testing.T) {
	rule := mapRule("listener-arn", awselbv2types.Rule{
		Actions: []awselbv2types.Action{{
			Type: awselbv2types.ActionTypeEnumForward,
			ForwardConfig: &awselbv2types.ForwardActionConfig{
				TargetGroups: []awselbv2types.TargetGroupTuple{{
					TargetGroupArn: aws.String("target-group-arn"),
					Weight:         aws.Int32(100),
				}},
			},
		}},
		Conditions: []awselbv2types.RuleCondition{{
			Field: aws.String("host-header"),
			HostHeaderConfig: &awselbv2types.HostHeaderConditionConfig{
				Values: []string{"api.example.com"},
			},
		}},
		Priority: aws.String("10"),
		RuleArn:  aws.String("rule-arn"),
	}, nil)

	if got := rule.Conditions[0].HostHeaderValues; strings.Join(got, ",") != "api.example.com" {
		t.Fatalf("host header values = %#v", got)
	}
	if got := rule.Actions[0].ForwardTargetGroups[0].ARN; got != "target-group-arn" {
		t.Fatalf("forward target group = %q", got)
	}
}

func TestChunkStringsSplitsDescribeTagsLimit(t *testing.T) {
	values := make([]string, 21)
	for index := range values {
		values[index] = "arn"
	}
	chunks := chunkStrings(values, describeTagsLimit)

	if len(chunks) != 2 {
		t.Fatalf("chunk count = %d, want 2", len(chunks))
	}
	if len(chunks[0]) != describeTagsLimit || len(chunks[1]) != 1 {
		t.Fatalf("chunks = %#v, want 20 then 1", chunks)
	}
}
