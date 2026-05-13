package awssdk

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsroute53 "github.com/aws/aws-sdk-go-v2/service/route53"
	awsroute53types "github.com/aws/aws-sdk-go-v2/service/route53/types"

	route53service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/route53"
)

var errUnexpectedMarker = errors.New("unexpected pagination marker")

func TestListHostedZonesPaginatesAndMapsTags(t *testing.T) {
	api := &fakeAPIClient{
		hostedZonePages: []*awsroute53.ListHostedZonesOutput{
			{
				HostedZones: []awsroute53types.HostedZone{{
					Id:   aws.String("/hostedzone/Z1"),
					Name: aws.String("example.com."),
					Config: &awsroute53types.HostedZoneConfig{
						Comment:     aws.String("public zone"),
						PrivateZone: false,
					},
					ResourceRecordSetCount: aws.Int64(2),
				}},
				IsTruncated: true,
				NextMarker:  aws.String("next"),
			},
			{
				HostedZones: []awsroute53types.HostedZone{{
					Id:   aws.String("/hostedzone/Z2"),
					Name: aws.String("svc.local."),
					Config: &awsroute53types.HostedZoneConfig{
						PrivateZone: true,
					},
				}},
			},
		},
		tags: map[string]*awsroute53.ListTagsForResourceOutput{
			"Z1": {
				ResourceTagSet: &awsroute53types.ResourceTagSet{
					Tags: []awsroute53types.Tag{{
						Key:   aws.String("team"),
						Value: aws.String("edge"),
					}},
				},
			},
		},
	}
	client := &Client{client: api}

	zones, err := client.ListHostedZones(context.Background())
	if err != nil {
		t.Fatalf("ListHostedZones returned error: %v", err)
	}
	if len(zones) != 2 {
		t.Fatalf("len(zones) = %d, want 2", len(zones))
	}
	if zones[0].Tags["team"] != "edge" {
		t.Fatalf("tag team = %q, want edge", zones[0].Tags["team"])
	}
	if !zones[1].Private {
		t.Fatalf("private zone = false, want true")
	}
	if got := api.tagResourceIDs[0]; got != "Z1" {
		t.Fatalf("tag resource id = %q, want trimmed hosted zone ID Z1", got)
	}
}

func TestListResourceRecordSetsPaginatesAndMapsAlias(t *testing.T) {
	api := &fakeAPIClient{
		recordPages: []*awsroute53.ListResourceRecordSetsOutput{
			{
				ResourceRecordSets: []awsroute53types.ResourceRecordSet{{
					Name: aws.String("www.example.com."),
					Type: awsroute53types.RRTypeA,
					AliasTarget: &awsroute53types.AliasTarget{
						DNSName:              aws.String("dualstack.api.elb.amazonaws.com."),
						HostedZoneId:         aws.String("Z35SXDOTRQ7X7K"),
						EvaluateTargetHealth: true,
					},
				}},
				IsTruncated:    true,
				NextRecordName: aws.String("api.example.com."),
				NextRecordType: awsroute53types.RRTypeAaaa,
			},
			{
				ResourceRecordSets: []awsroute53types.ResourceRecordSet{{
					Name: aws.String("api.example.com."),
					Type: awsroute53types.RRTypeAaaa,
					TTL:  aws.Int64(60),
					ResourceRecords: []awsroute53types.ResourceRecord{{
						Value: aws.String("2001:db8::1"),
					}},
				}},
			},
		},
	}
	client := &Client{client: api}

	records, err := client.ListResourceRecordSets(context.Background(), testHostedZone())
	if err != nil {
		t.Fatalf("ListResourceRecordSets returned error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[0].AliasTarget == nil {
		t.Fatalf("first record alias target = nil")
	}
	if got := records[0].AliasTarget.HostedZoneID; got != "Z35SXDOTRQ7X7K" {
		t.Fatalf("alias hosted zone = %q", got)
	}
	if got := records[1].Values[0]; got != "2001:db8::1" {
		t.Fatalf("AAAA value = %q", got)
	}
	if got := len(api.recordInputs); got != 2 {
		t.Fatalf("record page calls = %d, want 2", got)
	}
	if got := aws.ToString(api.recordInputs[1].StartRecordName); got != "api.example.com." {
		t.Fatalf("second page StartRecordName = %q", got)
	}
}

func testHostedZone() route53service.HostedZone {
	return route53service.HostedZone{
		ID:   "/hostedzone/Z1",
		Name: "example.com.",
	}
}

type fakeAPIClient struct {
	hostedZonePages []*awsroute53.ListHostedZonesOutput
	recordPages     []*awsroute53.ListResourceRecordSetsOutput
	tags            map[string]*awsroute53.ListTagsForResourceOutput
	hostedZoneCalls int
	recordCalls     int
	tagResourceIDs  []string
	recordInputs    []*awsroute53.ListResourceRecordSetsInput
}

func (c *fakeAPIClient) ListHostedZones(
	_ context.Context,
	input *awsroute53.ListHostedZonesInput,
	_ ...func(*awsroute53.Options),
) (*awsroute53.ListHostedZonesOutput, error) {
	if c.hostedZoneCalls >= len(c.hostedZonePages) {
		return &awsroute53.ListHostedZonesOutput{}, nil
	}
	if c.hostedZoneCalls == 1 && aws.ToString(input.Marker) != "next" {
		return nil, errUnexpectedMarker
	}
	page := c.hostedZonePages[c.hostedZoneCalls]
	c.hostedZoneCalls++
	return page, nil
}

func (c *fakeAPIClient) ListResourceRecordSets(
	_ context.Context,
	input *awsroute53.ListResourceRecordSetsInput,
	_ ...func(*awsroute53.Options),
) (*awsroute53.ListResourceRecordSetsOutput, error) {
	c.recordInputs = append(c.recordInputs, input)
	if c.recordCalls >= len(c.recordPages) {
		return &awsroute53.ListResourceRecordSetsOutput{}, nil
	}
	page := c.recordPages[c.recordCalls]
	c.recordCalls++
	return page, nil
}

func (c *fakeAPIClient) ListTagsForResource(
	_ context.Context,
	input *awsroute53.ListTagsForResourceInput,
	_ ...func(*awsroute53.Options),
) (*awsroute53.ListTagsForResourceOutput, error) {
	resourceID := aws.ToString(input.ResourceId)
	c.tagResourceIDs = append(c.tagResourceIDs, resourceID)
	if c.tags == nil {
		return &awsroute53.ListTagsForResourceOutput{}, nil
	}
	return c.tags[resourceID], nil
}
