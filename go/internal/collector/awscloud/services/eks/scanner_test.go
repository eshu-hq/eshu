package eks

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsEKSClusterNodegroupAddonAndOIDCEvidence(t *testing.T) {
	clusterARN := "arn:aws:eks:us-east-1:123456789012:cluster/prod"
	nodegroupARN := "arn:aws:eks:us-east-1:123456789012:nodegroup/prod/workers/11111111-2222-3333-4444-555555555555"
	addonARN := "arn:aws:eks:us-east-1:123456789012:addon/prod/vpc-cni/11111111-2222-3333-4444-555555555555"
	oidcARN := "arn:aws:iam::123456789012:oidc-provider/oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE"
	client := fakeClient{
		clusters: []Cluster{{
			ARN:      clusterARN,
			Name:     "prod",
			Version:  "1.30",
			Status:   "ACTIVE",
			RoleARN:  "arn:aws:iam::123456789012:role/eks-cluster",
			Endpoint: "https://ABCDEF.gr7.us-east-1.eks.amazonaws.com",
			OIDCProvider: &OIDCProvider{
				ARN:         oidcARN,
				IssuerURL:   "https://oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE",
				Thumbprints: []string{"9e99a48a9960b14926bb7f3b02e22da0afd10df6"},
				ClientIDs:   []string{"sts.amazonaws.com"},
			},
			VPCConfig: VPCConfig{
				VPCID:                  "vpc-123",
				SubnetIDs:              []string{"subnet-a", "subnet-b"},
				SecurityGroupIDs:       []string{"sg-control-plane"},
				ClusterSecurityGroupID: "sg-cluster",
				EndpointPublicAccess:   true,
			},
			Tags: map[string]string{"environment": "prod"},
		}},
		nodegroups: map[string][]Nodegroup{
			"prod": {{
				ARN:           nodegroupARN,
				Name:          "workers",
				ClusterName:   "prod",
				Version:       "1.30",
				Status:        "ACTIVE",
				NodeRoleARN:   "arn:aws:iam::123456789012:role/eks-workers",
				Subnets:       []string{"subnet-a", "subnet-b"},
				InstanceTypes: []string{"m7i.large"},
				ScalingConfig: ScalingConfig{DesiredSize: 3, MinSize: 2, MaxSize: 5},
				Tags:          map[string]string{"pool": "workers"},
			}},
		},
		addons: map[string][]Addon{
			"prod": {{
				ARN:                   addonARN,
				Name:                  "vpc-cni",
				ClusterName:           "prod",
				Version:               "v1.18.3-eksbuild.1",
				Status:                "ACTIVE",
				ServiceAccountRoleARN: "arn:aws:iam::123456789012:role/vpc-cni",
				Tags:                  map[string]string{"managed": "true"},
			}},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	assertResourceType(t, envelopes, awscloud.ResourceTypeEKSCluster)
	assertResourceType(t, envelopes, awscloud.ResourceTypeEKSNodegroup)
	addon := resourceByType(t, envelopes, awscloud.ResourceTypeEKSAddon)
	oidc := resourceByType(t, envelopes, awscloud.ResourceTypeEKSOIDCProvider)
	assertAttribute(t, addon, "addon_version", "v1.18.3-eksbuild.1")
	assertAttribute(t, oidc, "issuer_url", "https://oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE")
	assertRelationship(t, envelopes, awscloud.RelationshipEKSClusterHasOIDCProvider)
	assertRelationship(t, envelopes, awscloud.RelationshipEKSClusterHasNodegroup)
	assertRelationship(t, envelopes, awscloud.RelationshipEKSClusterHasAddon)
	assertRelationship(t, envelopes, awscloud.RelationshipEKSNodegroupUsesIAMRole)
	assertRelationship(t, envelopes, awscloud.RelationshipEKSAddonUsesIAMRole)
	assertRelationship(t, envelopes, awscloud.RelationshipEKSClusterUsesSubnet)
	assertRelationship(t, envelopes, awscloud.RelationshipEKSClusterUsesSecurityGroup)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceLambda
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerDeduplicatesClusterSecurityGroupRelationships(t *testing.T) {
	client := fakeClient{
		clusters: []Cluster{{
			ARN:  "arn:aws:eks:us-east-1:123456789012:cluster/prod",
			Name: "prod",
			VPCConfig: VPCConfig{
				ClusterSecurityGroupID: "sg-shared",
				SecurityGroupIDs:       []string{"sg-shared", "sg-extra"},
			},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipEKSClusterUsesSecurityGroup); got != 2 {
		t.Fatalf("security group relationship count = %d, want 2", got)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceEKS,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:eks:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	clusters   []Cluster
	nodegroups map[string][]Nodegroup
	addons     map[string][]Addon
}

func (c fakeClient) ListClusters(context.Context) ([]Cluster, error) {
	return c.clusters, nil
}

func (c fakeClient) ListNodegroups(_ context.Context, cluster Cluster) ([]Nodegroup, error) {
	return c.nodegroups[cluster.Name], nil
}

func (c fakeClient) ListAddons(_ context.Context, cluster Cluster) ([]Addon, error) {
	return c.addons[cluster.Name], nil
}

func assertResourceType(t *testing.T, envelopes []facts.Envelope, resourceType string) {
	t.Helper()
	_ = resourceByType(t, envelopes, resourceType)
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

func assertRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
}

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
	var count int
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

func assertAttribute(t *testing.T, envelope facts.Envelope, key string, want string) {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	if got := attributes[key]; got != want {
		t.Fatalf("attributes[%s] = %#v, want %q", key, got, want)
	}
}
