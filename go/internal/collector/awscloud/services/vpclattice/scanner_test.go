// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vpclattice

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testNetworkARN  = "arn:aws:vpc-lattice:us-east-1:123456789012:servicenetwork/sn-0123"
	testServiceARN  = "arn:aws:vpc-lattice:us-east-1:123456789012:service/svc-0123"
	testListenerARN = "arn:aws:vpc-lattice:us-east-1:123456789012:service/svc-0123/listener/listener-0123"
	testTGARN       = "arn:aws:vpc-lattice:us-east-1:123456789012:targetgroup/tg-0123"
	testLambdaARN   = "arn:aws:lambda:us-east-1:123456789012:function:checkout"
	testALBARN      = "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/0123"
	testCertARN     = "arn:aws:acm:us-east-1:123456789012:certificate/abcd-1234"
)

func TestScannerEmitsVPCLatticeMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		ServiceNetworks: []ServiceNetwork{{
			ARN:                        testNetworkARN,
			ID:                         "sn-0123",
			Name:                       "commerce-net",
			NumberOfAssociatedServices: 2,
			NumberOfAssociatedVPCs:     1,
			CreatedAt:                  time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:                       map[string]string{"Environment": "prod"},
			VPCAssociations: []VPCAssociation{{
				ID:     "snva-0123",
				VPCID:  "vpc-0a1b2c3d",
				Status: "ACTIVE",
			}},
			ServiceAssociations: []ServiceAssociation{{
				ID:         "snsa-0123",
				ServiceARN: testServiceARN,
				ServiceID:  "svc-0123",
				Status:     "ACTIVE",
			}},
		}},
		Services: []Service{{
			ARN:                testServiceARN,
			ID:                 "svc-0123",
			Name:               "checkout",
			Status:             "ACTIVE",
			CustomDomainName:   "checkout.example.com",
			DNSEntryDomainName: "checkout-0123.vpc-lattice-svcs.us-east-1.on.aws",
			AuthType:           "AWS_IAM",
			CertificateARN:     testCertARN,
			Tags:               map[string]string{"Team": "payments"},
			Listeners: []Listener{{
				ARN:      testListenerARN,
				ID:       "listener-0123",
				Name:     "https",
				Protocol: "HTTPS",
				Port:     443,
			}},
		}},
		TargetGroups: []TargetGroup{{
			ARN:         testTGARN,
			ID:          "tg-0123",
			Name:        "checkout-lambda",
			Type:        "LAMBDA",
			Status:      "ACTIVE",
			ServiceARNs: []string{testServiceARN},
			CreatedAt:   time.Date(2026, 5, 14, 12, 5, 0, 0, time.UTC),
			Tags:        map[string]string{"Team": "payments"},
			Targets:     []Target{{ID: testLambdaARN, Status: "HEALTHY"}},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Service network resource node.
	network := resourceByType(t, envelopes, awscloud.ResourceTypeVPCLatticeServiceNetwork)
	if got, want := network.Payload["resource_id"], testNetworkARN; got != want {
		t.Fatalf("service network resource_id = %#v, want %q", got, want)
	}
	networkAttrs := attributesOf(t, network)
	assertAttribute(t, networkAttrs, "number_of_associated_services", int64(2))
	assertAttribute(t, networkAttrs, "service_network_id", "sn-0123")

	// service network -> VPC edge, keyed by the bare vpc id the EC2 scanner publishes.
	netVPC := relationshipByType(t, envelopes, awscloud.RelationshipVPCLatticeServiceNetworkAssociatesVPC)
	assertEdgeTarget(t, netVPC, awscloud.ResourceTypeEC2VPC, "vpc-0a1b2c3d")
	if got := netVPC.Payload["target_arn"]; got != "" {
		t.Fatalf("service network -> vpc target_arn = %#v, want empty for bare vpc id", got)
	}

	// service network -> service edge, keyed by the service ARN the service node publishes.
	netSvc := relationshipByType(t, envelopes, awscloud.RelationshipVPCLatticeServiceNetworkAssociatesService)
	assertEdgeTarget(t, netSvc, awscloud.ResourceTypeVPCLatticeService, testServiceARN)
	if got, want := netSvc.Payload["target_arn"], testServiceARN; got != want {
		t.Fatalf("service network -> service target_arn = %#v, want %q", got, want)
	}

	// Service resource node.
	service := resourceByType(t, envelopes, awscloud.ResourceTypeVPCLatticeService)
	if got, want := service.Payload["resource_id"], testServiceARN; got != want {
		t.Fatalf("service resource_id = %#v, want %q", got, want)
	}
	if got, want := service.Payload["state"], "ACTIVE"; got != want {
		t.Fatalf("service state = %#v, want %q", got, want)
	}
	serviceAttrs := attributesOf(t, service)
	assertAttribute(t, serviceAttrs, "custom_domain_name", "checkout.example.com")
	assertAttribute(t, serviceAttrs, "auth_type", "AWS_IAM")

	// service -> ACM certificate edge.
	svcCert := relationshipByType(t, envelopes, awscloud.RelationshipVPCLatticeServiceUsesCertificate)
	assertEdgeTarget(t, svcCert, awscloud.ResourceTypeACMCertificate, testCertARN)
	if got, want := svcCert.Payload["target_arn"], testCertARN; got != want {
		t.Fatalf("service -> cert target_arn = %#v, want %q", got, want)
	}

	// Listener resource node + listener -> service edge.
	listener := resourceByType(t, envelopes, awscloud.ResourceTypeVPCLatticeListener)
	if got, want := listener.Payload["resource_id"], testListenerARN; got != want {
		t.Fatalf("listener resource_id = %#v, want %q", got, want)
	}
	listenerInSvc := relationshipByType(t, envelopes, awscloud.RelationshipVPCLatticeListenerInService)
	assertEdgeTarget(t, listenerInSvc, awscloud.ResourceTypeVPCLatticeService, testServiceARN)
	if got, want := listenerInSvc.Payload["source_resource_id"], testListenerARN; got != want {
		t.Fatalf("listener -> service source_resource_id = %#v, want %q", got, want)
	}

	// Target group resource node.
	group := resourceByType(t, envelopes, awscloud.ResourceTypeVPCLatticeTargetGroup)
	if got, want := group.Payload["resource_id"], testTGARN; got != want {
		t.Fatalf("target group resource_id = %#v, want %q", got, want)
	}
	groupAttrs := attributesOf(t, group)
	assertAttribute(t, groupAttrs, "type", "LAMBDA")
	assertAttribute(t, groupAttrs, "target_count", int64(1))

	// target group -> service edge.
	tgSvc := relationshipByType(t, envelopes, awscloud.RelationshipVPCLatticeTargetGroupServesService)
	assertEdgeTarget(t, tgSvc, awscloud.ResourceTypeVPCLatticeService, testServiceARN)

	// target group -> Lambda function edge.
	tgLambda := relationshipByType(t, envelopes, awscloud.RelationshipVPCLatticeTargetGroupTargetsLambda)
	assertEdgeTarget(t, tgLambda, awscloud.ResourceTypeLambdaFunction, testLambdaARN)
	if got, want := tgLambda.Payload["target_arn"], testLambdaARN; got != want {
		t.Fatalf("target group -> lambda target_arn = %#v, want %q", got, want)
	}

	// No auth-policy / resource-policy / data-plane leakage anywhere.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"auth_policy", "resource_policy", "policy", "policy_document",
			"auth_policy_document", "rules", "rule_body", "default_action",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; VPC Lattice scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerEmitsInstanceAndALBTargetEdges(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{TargetGroups: []TargetGroup{
		{
			ARN:     testTGARN + "-instance",
			ID:      "tg-instance",
			Name:    "web-instances",
			Type:    "INSTANCE",
			VPCID:   "vpc-0a1b2c3d",
			Status:  "ACTIVE",
			Targets: []Target{{ID: "i-0123456789abcdef0", Status: "HEALTHY"}},
		},
		{
			ARN:     testTGARN + "-alb",
			ID:      "tg-alb",
			Name:    "web-alb",
			Type:    "ALB",
			VPCID:   "vpc-0a1b2c3d",
			Status:  "ACTIVE",
			Targets: []Target{{ID: testALBARN, Status: "HEALTHY"}},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// target group -> VPC edge (bare vpc id).
	tgVPC := relationshipByType(t, envelopes, awscloud.RelationshipVPCLatticeTargetGroupInVPC)
	assertEdgeTarget(t, tgVPC, awscloud.ResourceTypeEC2VPC, "vpc-0a1b2c3d")

	// instance edge keyed by bare i-id, no synthesized ARN.
	tgInstance := relationshipByType(t, envelopes, awscloud.RelationshipVPCLatticeTargetGroupTargetsInstance)
	assertEdgeTarget(t, tgInstance, ec2InstanceTargetType, "i-0123456789abcdef0")
	if got := tgInstance.Payload["target_arn"]; got != "" {
		t.Fatalf("instance edge target_arn = %#v, want empty for bare instance id", got)
	}

	// ALB edge keyed by the ALB ARN the ELBv2 scanner publishes.
	tgALB := relationshipByType(t, envelopes, awscloud.RelationshipVPCLatticeTargetGroupTargetsLoadBalancer)
	assertEdgeTarget(t, tgALB, awscloud.ResourceTypeELBv2LoadBalancer, testALBARN)
	if got, want := tgALB.Payload["target_arn"], testALBARN; got != want {
		t.Fatalf("ALB edge target_arn = %#v, want %q", got, want)
	}
}

func TestScannerSkipsUnresolvableTargets(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{TargetGroups: []TargetGroup{
		{
			ARN:     testTGARN + "-ip",
			ID:      "tg-ip",
			Name:    "raw-ip",
			Type:    "IP",
			VPCID:   "vpc-0a1b2c3d",
			Status:  "ACTIVE",
			Targets: []Target{{ID: "10.0.1.5", Status: "HEALTHY"}},
		},
		{
			ARN:     testTGARN + "-badlambda",
			ID:      "tg-badlambda",
			Name:    "lambda-no-arn",
			Type:    "LAMBDA",
			Status:  "ACTIVE",
			Targets: []Target{{ID: "checkout", Status: "HEALTHY"}},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		switch envelope.Payload["relationship_type"] {
		case awscloud.RelationshipVPCLatticeTargetGroupTargetsLambda,
			awscloud.RelationshipVPCLatticeTargetGroupTargetsInstance,
			awscloud.RelationshipVPCLatticeTargetGroupTargetsLoadBalancer:
			t.Fatalf("unexpected unresolvable target edge: %#v", envelope.Payload)
		}
	}
}

func TestScannerSynthesizesNoARNForBareVPCInGovCloud(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	client := fakeClient{snapshot: Snapshot{ServiceNetworks: []ServiceNetwork{{
		ARN:  "arn:aws-us-gov:vpc-lattice:us-gov-west-1:123456789012:servicenetwork/sn-gov",
		ID:   "sn-gov",
		Name: "gov-net",
		VPCAssociations: []VPCAssociation{{
			ID:    "snva-gov",
			VPCID: "vpc-gov0123",
		}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	netVPC := relationshipByType(t, envelopes, awscloud.RelationshipVPCLatticeServiceNetworkAssociatesVPC)
	if got, want := netVPC.Payload["target_resource_id"], "vpc-gov0123"; got != want {
		t.Fatalf("GovCloud service network -> vpc target_resource_id = %#v, want %q", got, want)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		ServiceNetworks: []ServiceNetwork{{ARN: testNetworkARN, ID: "sn-0123", Name: "empty-net"}},
		Services:        []Service{{ARN: testServiceARN, ID: "svc-0123", Name: "no-cert"}},
		TargetGroups:    []TargetGroup{{ARN: testTGARN, ID: "tg-0123", Name: "no-vpc", Type: "LAMBDA"}},
	}}

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

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	network := ServiceNetwork{ARN: testNetworkARN, ID: "sn-0123", Name: "commerce-net"}
	service := Service{ARN: testServiceARN, ID: "svc-0123", Name: "checkout", CertificateARN: testCertARN}
	listener := Listener{ARN: testListenerARN, ID: "listener-0123", Name: "https"}
	lambdaGroup := TargetGroup{ARN: testTGARN, ID: "tg-0123", Name: "lambda-tg", Type: "LAMBDA", VPCID: "vpc-0a1b2c3d"}
	instanceGroup := TargetGroup{ARN: testTGARN + "-i", ID: "tg-i", Type: "INSTANCE", VPCID: "vpc-0a1b2c3d"}
	albGroup := TargetGroup{ARN: testTGARN + "-alb", ID: "tg-alb", Type: "ALB", VPCID: "vpc-0a1b2c3d"}

	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		serviceNetworkVPCRelationship(boundary, network, VPCAssociation{ID: "snva", VPCID: "vpc-0a1b2c3d"}),
		serviceNetworkServiceRelationship(boundary, network, ServiceAssociation{ID: "snsa", ServiceARN: testServiceARN}),
		listenerInServiceRelationship(boundary, service, listener),
		serviceCertificateRelationship(boundary, service),
		targetGroupVPCRelationship(boundary, lambdaGroup),
		targetGroupServiceRelationship(boundary, lambdaGroup, testServiceARN),
		targetGroupTargetRelationship(boundary, lambdaGroup, Target{ID: testLambdaARN}),
		targetGroupTargetRelationship(boundary, instanceGroup, Target{ID: "i-0123456789abcdef0"}),
		targetGroupTargetRelationship(boundary, albGroup, Target{ID: testALBARN}),
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

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		ServiceNetworks: []ServiceNetwork{{ARN: testNetworkARN, ID: "sn-0123", Name: "commerce-net"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "VPC Lattice ListTargetGroups throttled after SDK retries; target group metadata omitted",
			SourceRecordID: "vpclattice_target_groups_throttled",
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

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing-client error")
	}
}
