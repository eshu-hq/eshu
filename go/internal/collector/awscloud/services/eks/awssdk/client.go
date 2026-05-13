package awssdk

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awseks "github.com/aws/aws-sdk-go-v2/service/eks"
	awsiam "github.com/aws/aws-sdk-go-v2/service/iam"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	eksservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/eks"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type apiClient interface {
	DescribeAddon(context.Context, *awseks.DescribeAddonInput, ...func(*awseks.Options)) (*awseks.DescribeAddonOutput, error)
	DescribeCluster(context.Context, *awseks.DescribeClusterInput, ...func(*awseks.Options)) (*awseks.DescribeClusterOutput, error)
	DescribeNodegroup(context.Context, *awseks.DescribeNodegroupInput, ...func(*awseks.Options)) (*awseks.DescribeNodegroupOutput, error)
	awseks.ListAddonsAPIClient
	awseks.ListClustersAPIClient
	awseks.ListNodegroupsAPIClient
}

type iamAPIClient interface {
	GetOpenIDConnectProvider(context.Context, *awsiam.GetOpenIDConnectProviderInput, ...func(*awsiam.Options)) (*awsiam.GetOpenIDConnectProviderOutput, error)
	ListOpenIDConnectProviders(context.Context, *awsiam.ListOpenIDConnectProvidersInput, ...func(*awsiam.Options)) (*awsiam.ListOpenIDConnectProvidersOutput, error)
}

// Client adapts AWS SDK EKS and IAM reads into scanner-owned EKS records.
type Client struct {
	client      apiClient
	iamClient   iamAPIClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments

	oidcProvidersLoaded bool
	oidcProviders       []oidcProviderRecord
}

// NewClient builds an EKS SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awseks.NewFromConfig(config),
		iamClient:   awsiam.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListClusters returns all EKS clusters visible to the configured AWS
// credentials, enriched with IAM OIDC provider evidence when it is available.
func (c *Client) ListClusters(ctx context.Context) ([]eksservice.Cluster, error) {
	paginator := awseks.NewListClustersPaginator(c.client, &awseks.ListClustersInput{})
	var clusters []eksservice.Cluster
	for paginator.HasMorePages() {
		var page *awseks.ListClustersOutput
		err := c.recordAPICall(ctx, "ListClusters", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, name := range page.Clusters {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			var output *awseks.DescribeClusterOutput
			err := c.recordAPICall(ctx, "DescribeCluster", func(callCtx context.Context) error {
				var err error
				output, err = c.client.DescribeCluster(callCtx, &awseks.DescribeClusterInput{
					Name: aws.String(name),
				})
				return err
			})
			if err != nil {
				return nil, fmt.Errorf("describe EKS cluster %q: %w", name, err)
			}
			if output == nil || output.Cluster == nil {
				continue
			}
			cluster, err := c.mapCluster(ctx, *output.Cluster)
			if err != nil {
				return nil, err
			}
			clusters = append(clusters, cluster)
		}
	}
	return clusters, nil
}

// ListNodegroups returns managed node groups for one EKS cluster.
func (c *Client) ListNodegroups(
	ctx context.Context,
	cluster eksservice.Cluster,
) ([]eksservice.Nodegroup, error) {
	clusterName := strings.TrimSpace(cluster.Name)
	if clusterName == "" {
		return nil, nil
	}
	paginator := awseks.NewListNodegroupsPaginator(c.client, &awseks.ListNodegroupsInput{
		ClusterName: aws.String(clusterName),
	})
	var nodegroups []eksservice.Nodegroup
	for paginator.HasMorePages() {
		var page *awseks.ListNodegroupsOutput
		err := c.recordAPICall(ctx, "ListNodegroups", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, name := range page.Nodegroups {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			var output *awseks.DescribeNodegroupOutput
			err := c.recordAPICall(ctx, "DescribeNodegroup", func(callCtx context.Context) error {
				var err error
				output, err = c.client.DescribeNodegroup(callCtx, &awseks.DescribeNodegroupInput{
					ClusterName:   aws.String(clusterName),
					NodegroupName: aws.String(name),
				})
				return err
			})
			if err != nil {
				return nil, fmt.Errorf("describe EKS nodegroup %q/%q: %w", clusterName, name, err)
			}
			if output == nil || output.Nodegroup == nil {
				continue
			}
			nodegroups = append(nodegroups, mapNodegroup(*output.Nodegroup))
		}
	}
	return nodegroups, nil
}

// ListAddons returns managed add-ons for one EKS cluster.
func (c *Client) ListAddons(ctx context.Context, cluster eksservice.Cluster) ([]eksservice.Addon, error) {
	clusterName := strings.TrimSpace(cluster.Name)
	if clusterName == "" {
		return nil, nil
	}
	paginator := awseks.NewListAddonsPaginator(c.client, &awseks.ListAddonsInput{
		ClusterName: aws.String(clusterName),
	})
	var addons []eksservice.Addon
	for paginator.HasMorePages() {
		var page *awseks.ListAddonsOutput
		err := c.recordAPICall(ctx, "ListAddons", func(callCtx context.Context) error {
			var err error
			page, err = paginator.NextPage(callCtx)
			return err
		})
		if err != nil {
			return nil, err
		}
		for _, name := range page.Addons {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			var output *awseks.DescribeAddonOutput
			err := c.recordAPICall(ctx, "DescribeAddon", func(callCtx context.Context) error {
				var err error
				output, err = c.client.DescribeAddon(callCtx, &awseks.DescribeAddonInput{
					AddonName:   aws.String(name),
					ClusterName: aws.String(clusterName),
				})
				return err
			})
			if err != nil {
				return nil, fmt.Errorf("describe EKS addon %q/%q: %w", clusterName, name, err)
			}
			if output == nil || output.Addon == nil {
				continue
			}
			addons = append(addons, mapAddon(*output.Addon))
		}
	}
	return addons, nil
}

var _ eksservice.Client = (*Client)(nil)

var _ apiClient = (*awseks.Client)(nil)

var _ iamAPIClient = (*awsiam.Client)(nil)
