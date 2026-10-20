// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsvpclattice "github.com/aws/aws-sdk-go-v2/service/vpclattice"
	awsvpclatticetypes "github.com/aws/aws-sdk-go-v2/service/vpclattice/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsVPCLatticeMetadataOnly(t *testing.T) {
	const (
		networkARN = "arn:aws:vpc-lattice:us-east-1:123456789012:servicenetwork/sn-0123"
		serviceARN = "arn:aws:vpc-lattice:us-east-1:123456789012:service/svc-0123"
		tgARN      = "arn:aws:vpc-lattice:us-east-1:123456789012:targetgroup/tg-0123"
		certARN    = "arn:aws:acm:us-east-1:123456789012:certificate/abcd"
		lambdaARN  = "arn:aws:lambda:us-east-1:123456789012:function:checkout"
	)

	api := &fakeVPCLatticeAPI{
		networks: []awsvpclatticetypes.ServiceNetworkSummary{{
			Arn:                        aws.String(networkARN),
			Id:                         aws.String("sn-0123"),
			Name:                       aws.String("commerce-net"),
			NumberOfAssociatedServices: aws.Int64(1),
			NumberOfAssociatedVPCs:     aws.Int64(1),
		}},
		vpcAssociations: map[string][]awsvpclatticetypes.ServiceNetworkVpcAssociationSummary{
			"sn-0123": {{
				Id:     aws.String("snva-0123"),
				VpcId:  aws.String("vpc-0a1b2c3d"),
				Status: awsvpclatticetypes.ServiceNetworkVpcAssociationStatusActive,
			}},
		},
		serviceAssociations: map[string][]awsvpclatticetypes.ServiceNetworkServiceAssociationSummary{
			"sn-0123": {{
				Id:         aws.String("snsa-0123"),
				ServiceArn: aws.String(serviceARN),
				ServiceId:  aws.String("svc-0123"),
				Status:     awsvpclatticetypes.ServiceNetworkServiceAssociationStatusActive,
			}},
		},
		services: []awsvpclatticetypes.ServiceSummary{{
			Arn:              aws.String(serviceARN),
			Id:               aws.String("svc-0123"),
			Name:             aws.String("checkout"),
			Status:           awsvpclatticetypes.ServiceStatusActive,
			CustomDomainName: aws.String("checkout.example.com"),
			DnsEntry:         &awsvpclatticetypes.DnsEntry{DomainName: aws.String("checkout.on.aws")},
		}},
		getService: map[string]*awsvpclattice.GetServiceOutput{
			"svc-0123": {
				Arn:            aws.String(serviceARN),
				Id:             aws.String("svc-0123"),
				AuthType:       awsvpclatticetypes.AuthTypeAwsIam,
				CertificateArn: aws.String(certARN),
			},
		},
		listeners: map[string][]awsvpclatticetypes.ListenerSummary{
			"svc-0123": {{
				Arn:      aws.String(serviceARN + "/listener/listener-0123"),
				Id:       aws.String("listener-0123"),
				Name:     aws.String("https"),
				Protocol: awsvpclatticetypes.ListenerProtocolHttps,
				Port:     aws.Int32(443),
			}},
		},
		targetGroups: []awsvpclatticetypes.TargetGroupSummary{{
			Arn:         aws.String(tgARN),
			Id:          aws.String("tg-0123"),
			Name:        aws.String("checkout-lambda"),
			Type:        awsvpclatticetypes.TargetGroupTypeLambda,
			Status:      awsvpclatticetypes.TargetGroupStatusActive,
			ServiceArns: []string{serviceARN},
		}},
		getTargetGroup: map[string]*awsvpclattice.GetTargetGroupOutput{
			"tg-0123": {
				Arn:  aws.String(tgARN),
				Id:   aws.String("tg-0123"),
				Type: awsvpclatticetypes.TargetGroupTypeLambda,
			},
		},
		targets: map[string][]awsvpclatticetypes.TargetSummary{
			"tg-0123": {{
				Id:     aws.String(lambdaARN),
				Status: awsvpclatticetypes.TargetStatusHealthy,
			}},
		},
		tags: map[string]map[string]string{
			networkARN: {"Environment": "prod"},
			serviceARN: {"Team": "payments"},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if len(snapshot.ServiceNetworks) != 1 {
		t.Fatalf("len(ServiceNetworks) = %d, want 1", len(snapshot.ServiceNetworks))
	}
	network := snapshot.ServiceNetworks[0]
	if network.ARN != networkARN {
		t.Fatalf("network ARN = %q, want %q", network.ARN, networkARN)
	}
	if network.Tags["Environment"] != "prod" {
		t.Fatalf("network tag Environment = %q, want prod", network.Tags["Environment"])
	}
	if len(network.VPCAssociations) != 1 || network.VPCAssociations[0].VPCID != "vpc-0a1b2c3d" {
		t.Fatalf("network VPC associations = %#v, want [vpc-0a1b2c3d]", network.VPCAssociations)
	}
	if len(network.ServiceAssociations) != 1 || network.ServiceAssociations[0].ServiceARN != serviceARN {
		t.Fatalf("network service associations = %#v, want [%s]", network.ServiceAssociations, serviceARN)
	}

	if len(snapshot.Services) != 1 {
		t.Fatalf("len(Services) = %d, want 1", len(snapshot.Services))
	}
	service := snapshot.Services[0]
	if service.AuthType != "AWS_IAM" {
		t.Fatalf("service AuthType = %q, want AWS_IAM (from GetService)", service.AuthType)
	}
	if service.CertificateARN != certARN {
		t.Fatalf("service CertificateARN = %q, want %q (from GetService)", service.CertificateARN, certARN)
	}
	if service.DNSEntryDomainName != "checkout.on.aws" {
		t.Fatalf("service DNSEntryDomainName = %q, want checkout.on.aws", service.DNSEntryDomainName)
	}
	if len(service.Listeners) != 1 || service.Listeners[0].Port != 443 {
		t.Fatalf("service listeners = %#v, want one HTTPS listener on 443", service.Listeners)
	}

	if len(snapshot.TargetGroups) != 1 {
		t.Fatalf("len(TargetGroups) = %d, want 1", len(snapshot.TargetGroups))
	}
	group := snapshot.TargetGroups[0]
	if group.Type != "LAMBDA" {
		t.Fatalf("target group Type = %q, want LAMBDA", group.Type)
	}
	if len(group.ServiceARNs) != 1 || group.ServiceARNs[0] != serviceARN {
		t.Fatalf("target group ServiceARNs = %#v, want [%s]", group.ServiceARNs, serviceARN)
	}
	if len(group.Targets) != 1 || group.Targets[0].ID != lambdaARN {
		t.Fatalf("target group Targets = %#v, want [%s]", group.Targets, lambdaARN)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceVPCLattice,
	}
}
