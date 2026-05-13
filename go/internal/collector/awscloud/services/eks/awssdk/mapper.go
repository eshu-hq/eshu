package awssdk

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"

	eksservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/eks"
)

func (c *Client) mapCluster(ctx context.Context, cluster awsekstypes.Cluster) (eksservice.Cluster, error) {
	mapped := eksservice.Cluster{
		ARN:             aws.ToString(cluster.Arn),
		CreatedAt:       aws.ToTime(cluster.CreatedAt),
		Endpoint:        aws.ToString(cluster.Endpoint),
		Name:            aws.ToString(cluster.Name),
		PlatformVersion: aws.ToString(cluster.PlatformVersion),
		RoleARN:         aws.ToString(cluster.RoleArn),
		Status:          string(cluster.Status),
		Tags:            cloneStringMap(cluster.Tags),
		Version:         aws.ToString(cluster.Version),
		VPCConfig:       mapVPCConfig(cluster.ResourcesVpcConfig),
	}
	issuerURL := clusterOIDCIssuer(cluster)
	if issuerURL == "" {
		return mapped, nil
	}
	providers, err := c.listOIDCProviderRecords(ctx)
	if err != nil {
		return eksservice.Cluster{}, fmt.Errorf("list IAM OIDC providers for EKS cluster %q: %w", mapped.Name, err)
	}
	if provider, ok := matchOIDCProvider(issuerURL, providers); ok {
		mapped.OIDCProvider = &provider
	} else {
		mapped.OIDCProvider = &eksservice.OIDCProvider{IssuerURL: issuerURL}
	}
	return mapped, nil
}

func mapVPCConfig(config *awsekstypes.VpcConfigResponse) eksservice.VPCConfig {
	if config == nil {
		return eksservice.VPCConfig{}
	}
	return eksservice.VPCConfig{
		ClusterSecurityGroupID: aws.ToString(config.ClusterSecurityGroupId),
		EndpointPrivateAccess:  config.EndpointPrivateAccess,
		EndpointPublicAccess:   config.EndpointPublicAccess,
		PublicAccessCIDRs:      cloneStrings(config.PublicAccessCidrs),
		SecurityGroupIDs:       cloneStrings(config.SecurityGroupIds),
		SubnetIDs:              cloneStrings(config.SubnetIds),
		VPCID:                  aws.ToString(config.VpcId),
	}
}

func mapNodegroup(nodegroup awsekstypes.Nodegroup) eksservice.Nodegroup {
	return eksservice.Nodegroup{
		AMIType:        string(nodegroup.AmiType),
		ARN:            aws.ToString(nodegroup.NodegroupArn),
		CapacityType:   string(nodegroup.CapacityType),
		ClusterName:    aws.ToString(nodegroup.ClusterName),
		InstanceTypes:  cloneStrings(nodegroup.InstanceTypes),
		Name:           aws.ToString(nodegroup.NodegroupName),
		NodeRoleARN:    aws.ToString(nodegroup.NodeRole),
		ReleaseVersion: aws.ToString(nodegroup.ReleaseVersion),
		ScalingConfig:  mapScalingConfig(nodegroup.ScalingConfig),
		Status:         string(nodegroup.Status),
		Subnets:        cloneStrings(nodegroup.Subnets),
		Tags:           cloneStringMap(nodegroup.Tags),
		Version:        aws.ToString(nodegroup.Version),
	}
}

func mapScalingConfig(config *awsekstypes.NodegroupScalingConfig) eksservice.ScalingConfig {
	if config == nil {
		return eksservice.ScalingConfig{}
	}
	return eksservice.ScalingConfig{
		DesiredSize: aws.ToInt32(config.DesiredSize),
		MaxSize:     aws.ToInt32(config.MaxSize),
		MinSize:     aws.ToInt32(config.MinSize),
	}
}

func mapAddon(addon awsekstypes.Addon) eksservice.Addon {
	return eksservice.Addon{
		ARN:                   aws.ToString(addon.AddonArn),
		ClusterName:           aws.ToString(addon.ClusterName),
		CreatedAt:             aws.ToTime(addon.CreatedAt),
		ModifiedAt:            aws.ToTime(addon.ModifiedAt),
		Name:                  aws.ToString(addon.AddonName),
		ServiceAccountRoleARN: aws.ToString(addon.ServiceAccountRoleArn),
		Status:                string(addon.Status),
		Tags:                  cloneStringMap(addon.Tags),
		Version:               aws.ToString(addon.AddonVersion),
	}
}

func clusterOIDCIssuer(cluster awsekstypes.Cluster) string {
	if cluster.Identity == nil || cluster.Identity.Oidc == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(cluster.Identity.Oidc.Issuer))
}
