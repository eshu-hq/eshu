package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awseks "github.com/aws/aws-sdk-go-v2/service/eks"
	awsekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	awsiam "github.com/aws/aws-sdk-go-v2/service/iam"
	awsiamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

func TestListClustersDescribesClustersAndAttachesOIDCProvider(t *testing.T) {
	createdAt := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	eksClient := &fakeEKSClient{
		clusterNames: []string{"prod"},
		clusters: map[string]*awseks.DescribeClusterOutput{
			"prod": {Cluster: &awsekstypes.Cluster{
				Arn:             aws.String("arn:aws:eks:us-east-1:123456789012:cluster/prod"),
				CreatedAt:       aws.Time(createdAt),
				Endpoint:        aws.String("https://ABCDEF.gr7.us-east-1.eks.amazonaws.com"),
				Identity:        &awsekstypes.Identity{Oidc: &awsekstypes.OIDC{Issuer: aws.String("https://oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE")}},
				Name:            aws.String("prod"),
				PlatformVersion: aws.String("eks.7"),
				ResourcesVpcConfig: &awsekstypes.VpcConfigResponse{
					ClusterSecurityGroupId: aws.String("sg-cluster"),
					EndpointPublicAccess:   true,
					PublicAccessCidrs:      []string{"203.0.113.0/24"},
					SecurityGroupIds:       []string{"sg-control-plane"},
					SubnetIds:              []string{"subnet-a", "subnet-b"},
					VpcId:                  aws.String("vpc-123"),
				},
				RoleArn: aws.String("arn:aws:iam::123456789012:role/eks-cluster"),
				Status:  awsekstypes.ClusterStatusActive,
				Tags:    map[string]string{"environment": "prod"},
				Version: aws.String("1.30"),
			}},
		},
	}
	iamClient := &fakeIAMClient{
		providerARNs: []string{"arn:aws:iam::123456789012:oidc-provider/oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE"},
		providers: map[string]*awsiam.GetOpenIDConnectProviderOutput{
			"arn:aws:iam::123456789012:oidc-provider/oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE": {
				ClientIDList:   []string{"sts.amazonaws.com"},
				ThumbprintList: []string{"9e99a48a9960b14926bb7f3b02e22da0afd10df6"},
				Url:            aws.String("oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE"),
			},
		},
	}

	clusters, err := (&Client{client: eksClient, iamClient: iamClient}).ListClusters(context.Background())
	if err != nil {
		t.Fatalf("ListClusters() error = %v", err)
	}
	if len(clusters) != 1 {
		t.Fatalf("cluster count = %d, want 1", len(clusters))
	}
	cluster := clusters[0]
	if cluster.Name != "prod" || cluster.Version != "1.30" || cluster.VPCConfig.VPCID != "vpc-123" {
		t.Fatalf("cluster = %#v", cluster)
	}
	if cluster.OIDCProvider == nil {
		t.Fatalf("OIDCProvider = nil, want provider evidence")
	}
	if cluster.OIDCProvider.ARN != "arn:aws:iam::123456789012:oidc-provider/oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE" {
		t.Fatalf("OIDCProvider.ARN = %q", cluster.OIDCProvider.ARN)
	}
	if cluster.OIDCProvider.IssuerURL != "https://oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE" {
		t.Fatalf("OIDCProvider.IssuerURL = %q", cluster.OIDCProvider.IssuerURL)
	}
	if got := cluster.OIDCProvider.Thumbprints; len(got) != 1 || got[0] != "9e99a48a9960b14926bb7f3b02e22da0afd10df6" {
		t.Fatalf("OIDCProvider.Thumbprints = %#v", got)
	}
	if got := cluster.OIDCProvider.ClientIDs; len(got) != 1 || got[0] != "sts.amazonaws.com" {
		t.Fatalf("OIDCProvider.ClientIDs = %#v", got)
	}
}

func TestMatchOIDCProviderNormalizesHTTPSIssuerAndARNPath(t *testing.T) {
	providers := []oidcProviderRecord{{
		ARN:         "arn:aws:iam::123456789012:oidc-provider/oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE",
		IssuerURL:   "oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE",
		Thumbprints: []string{"thumbprint"},
		ClientIDs:   []string{"sts.amazonaws.com"},
	}}

	provider, ok := matchOIDCProvider("https://oidc.eks.us-east-1.amazonaws.com/id/EXAMPLE", providers)
	if !ok {
		t.Fatalf("matchOIDCProvider() ok = false, want true")
	}
	if provider.ARN != providers[0].ARN {
		t.Fatalf("ARN = %q, want %q", provider.ARN, providers[0].ARN)
	}
}

func TestMapNodegroupPreservesRoleSubnetsAndScaling(t *testing.T) {
	nodegroup := mapNodegroup(awsekstypes.Nodegroup{
		AmiType:        awsekstypes.AMITypesAl2X8664,
		CapacityType:   awsekstypes.CapacityTypesOnDemand,
		ClusterName:    aws.String("prod"),
		InstanceTypes:  []string{"m7i.large"},
		NodeRole:       aws.String("arn:aws:iam::123456789012:role/eks-workers"),
		NodegroupArn:   aws.String("arn:aws:eks:us-east-1:123456789012:nodegroup/prod/workers/id"),
		NodegroupName:  aws.String("workers"),
		ReleaseVersion: aws.String("1.30.0-20260513"),
		ScalingConfig:  &awsekstypes.NodegroupScalingConfig{DesiredSize: aws.Int32(3), MinSize: aws.Int32(2), MaxSize: aws.Int32(5)},
		Status:         awsekstypes.NodegroupStatusActive,
		Subnets:        []string{"subnet-a", "subnet-b"},
		Version:        aws.String("1.30"),
	})

	if nodegroup.NodeRoleARN != "arn:aws:iam::123456789012:role/eks-workers" {
		t.Fatalf("NodeRoleARN = %q", nodegroup.NodeRoleARN)
	}
	if nodegroup.ScalingConfig.DesiredSize != 3 || nodegroup.ScalingConfig.MinSize != 2 || nodegroup.ScalingConfig.MaxSize != 5 {
		t.Fatalf("ScalingConfig = %#v", nodegroup.ScalingConfig)
	}
	if len(nodegroup.Subnets) != 2 {
		t.Fatalf("Subnets = %#v", nodegroup.Subnets)
	}
}

type fakeEKSClient struct {
	clusterNames []string
	clusters     map[string]*awseks.DescribeClusterOutput
	nodegroups   map[string][]string
	addons       map[string][]string
}

func (c *fakeEKSClient) ListClusters(
	context.Context,
	*awseks.ListClustersInput,
	...func(*awseks.Options),
) (*awseks.ListClustersOutput, error) {
	return &awseks.ListClustersOutput{Clusters: c.clusterNames}, nil
}

func (c *fakeEKSClient) DescribeCluster(
	_ context.Context,
	input *awseks.DescribeClusterInput,
	_ ...func(*awseks.Options),
) (*awseks.DescribeClusterOutput, error) {
	return c.clusters[aws.ToString(input.Name)], nil
}

func (c *fakeEKSClient) ListNodegroups(
	_ context.Context,
	input *awseks.ListNodegroupsInput,
	_ ...func(*awseks.Options),
) (*awseks.ListNodegroupsOutput, error) {
	return &awseks.ListNodegroupsOutput{Nodegroups: c.nodegroups[aws.ToString(input.ClusterName)]}, nil
}

func (c *fakeEKSClient) DescribeNodegroup(
	context.Context,
	*awseks.DescribeNodegroupInput,
	...func(*awseks.Options),
) (*awseks.DescribeNodegroupOutput, error) {
	return &awseks.DescribeNodegroupOutput{}, nil
}

func (c *fakeEKSClient) ListAddons(
	_ context.Context,
	input *awseks.ListAddonsInput,
	_ ...func(*awseks.Options),
) (*awseks.ListAddonsOutput, error) {
	return &awseks.ListAddonsOutput{Addons: c.addons[aws.ToString(input.ClusterName)]}, nil
}

func (c *fakeEKSClient) DescribeAddon(
	context.Context,
	*awseks.DescribeAddonInput,
	...func(*awseks.Options),
) (*awseks.DescribeAddonOutput, error) {
	return &awseks.DescribeAddonOutput{}, nil
}

type fakeIAMClient struct {
	providerARNs []string
	providers    map[string]*awsiam.GetOpenIDConnectProviderOutput
}

func (c *fakeIAMClient) ListOpenIDConnectProviders(
	context.Context,
	*awsiam.ListOpenIDConnectProvidersInput,
	...func(*awsiam.Options),
) (*awsiam.ListOpenIDConnectProvidersOutput, error) {
	providers := make([]awsiamtypes.OpenIDConnectProviderListEntry, 0, len(c.providerARNs))
	for _, arn := range c.providerARNs {
		providers = append(providers, awsiamtypes.OpenIDConnectProviderListEntry{Arn: aws.String(arn)})
	}
	return &awsiam.ListOpenIDConnectProvidersOutput{OpenIDConnectProviderList: providers}, nil
}

func (c *fakeIAMClient) GetOpenIDConnectProvider(
	_ context.Context,
	input *awsiam.GetOpenIDConnectProviderInput,
	_ ...func(*awsiam.Options),
) (*awsiam.GetOpenIDConnectProviderOutput, error) {
	return c.providers[aws.ToString(input.OpenIDConnectProviderArn)], nil
}
