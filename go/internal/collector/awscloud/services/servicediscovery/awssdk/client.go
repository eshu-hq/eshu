// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssd "github.com/aws/aws-sdk-go-v2/service/servicediscovery"
	sdtypes "github.com/aws/aws-sdk-go-v2/service/servicediscovery/types"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	sdservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/servicediscovery"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// apiClient is the subset of the AWS SDK Cloud Map (Service Discovery) client
// this adapter consumes. It is read-only and intentionally minimal: it lists
// namespaces, lists services per namespace, and reads tags. The interface
// deliberately excludes every Cloud Map mutation API (Create/Update/Delete for
// namespaces and services, RegisterInstance, DeregisterInstance,
// UpdateInstanceCustomHealthStatus, TagResource, UntagResource) and every
// instance discovery/read API (DiscoverInstances, DiscoverInstancesRevision,
// GetInstance, ListInstances, GetInstancesHealthStatus) that exposes instance
// attribute maps, which can carry caller-defined secrets. A reflection test
// asserts the exclusion so a future SDK refactor cannot quietly broaden the
// contract.
type apiClient interface {
	ListNamespaces(context.Context, *awssd.ListNamespacesInput, ...func(*awssd.Options)) (*awssd.ListNamespacesOutput, error)
	ListServices(context.Context, *awssd.ListServicesInput, ...func(*awssd.Options)) (*awssd.ListServicesOutput, error)
	ListTagsForResource(context.Context, *awssd.ListTagsForResourceInput, ...func(*awssd.Options)) (*awssd.ListTagsForResourceOutput, error)
}

// Client adapts AWS SDK Cloud Map control-plane calls into scanner-owned
// metadata. It pages ListNamespaces, fans out to a NAMESPACE_ID-filtered
// ListServices per namespace, reads tags per resource, records instance counts
// from the service summary, and never calls a mutation API or an instance
// attribute reader.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Cloud Map SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awssd.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListNamespaceInventory returns every Cloud Map namespace visible to the
// configured credentials, with services resolved to metadata-only records.
func (c *Client) ListNamespaceInventory(ctx context.Context) ([]sdservice.Namespace, error) {
	summaries, err := c.listNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	namespaces := make([]sdservice.Namespace, 0, len(summaries))
	for _, summary := range summaries {
		namespaceID := strings.TrimSpace(aws.ToString(summary.Id))
		if namespaceID == "" {
			continue
		}
		namespace := mapNamespace(summary)
		// Normalize the id and name to the validated, trimmed values so the
		// resource, the ListServices NAMESPACE_ID filter, and the attached
		// services all key on the same identity. A raw SDK value could carry
		// surrounding whitespace and would otherwise filter ListServices with a
		// value that never matches the validated id.
		namespace.ID = namespaceID
		namespace.Name = strings.TrimSpace(namespace.Name)
		namespace.Tags = c.resourceTagsOrEmpty(ctx, namespace.ARN)

		services, err := c.servicesForNamespace(ctx, namespace.ID, namespace.Name)
		if err != nil {
			return nil, err
		}
		namespace.Services = services
		namespaces = append(namespaces, namespace)
	}
	return namespaces, nil
}

func (c *Client) listNamespaces(ctx context.Context) ([]sdtypes.NamespaceSummary, error) {
	paginator := awssd.NewListNamespacesPaginator(c.client, &awssd.ListNamespacesInput{})
	var summaries []sdtypes.NamespaceSummary
	for paginator.HasMorePages() {
		var page *awssd.ListNamespacesOutput
		err := c.recordAPICall(ctx, "ListNamespaces", func(callCtx context.Context) error {
			var callErr error
			page, callErr = paginator.NextPage(callCtx)
			return callErr
		})
		if err != nil {
			return nil, fmt.Errorf("list Cloud Map namespaces: %w", err)
		}
		summaries = append(summaries, page.Namespaces...)
	}
	return summaries, nil
}

// servicesForNamespace lists every service in one namespace using a NAMESPACE_ID
// filter so each service carries its parent namespace association. The instance
// count is read from the service summary; instance attribute maps are never
// read.
func (c *Client) servicesForNamespace(ctx context.Context, namespaceID, namespaceName string) ([]sdservice.Service, error) {
	input := &awssd.ListServicesInput{
		Filters: []sdtypes.ServiceFilter{{
			Name:      sdtypes.ServiceFilterNameNamespaceId,
			Values:    []string{namespaceID},
			Condition: sdtypes.FilterConditionEq,
		}},
	}
	paginator := awssd.NewListServicesPaginator(c.client, input)
	var services []sdservice.Service
	for paginator.HasMorePages() {
		var page *awssd.ListServicesOutput
		err := c.recordAPICall(ctx, "ListServices", func(callCtx context.Context) error {
			var callErr error
			page, callErr = paginator.NextPage(callCtx)
			return callErr
		})
		if err != nil {
			return nil, fmt.Errorf("list Cloud Map services for namespace %q: %w", namespaceID, err)
		}
		for _, summary := range page.Services {
			service := mapService(summary, namespaceID, namespaceName)
			service.Tags = c.resourceTagsOrEmpty(ctx, service.ARN)
			services = append(services, service)
		}
	}
	return services, nil
}

// resourceTagsOrEmpty reads tags for one resource ARN, returning nil on a blank
// ARN. A tag read failure must not drop the resource; the API-call counter
// records the error result for operator visibility.
func (c *Client) resourceTagsOrEmpty(ctx context.Context, arn string) map[string]string {
	arn = strings.TrimSpace(arn)
	if arn == "" {
		return nil
	}
	tags, err := c.resourceTags(ctx, arn)
	if err != nil {
		return nil
	}
	return tags
}

// resourceTags reads the tags for one resource ARN. The Cloud Map
// ListTagsForResource API returns the full tag set in a single response, so
// there is no pagination here.
func (c *Client) resourceTags(ctx context.Context, arn string) (map[string]string, error) {
	var page *awssd.ListTagsForResourceOutput
	err := c.recordAPICall(ctx, "ListTagsForResource", func(callCtx context.Context) error {
		var callErr error
		page, callErr = c.client.ListTagsForResource(callCtx, &awssd.ListTagsForResourceInput{
			ResourceARN: aws.String(arn),
		})
		return callErr
	})
	if err != nil {
		return nil, err
	}
	if page == nil {
		return nil, nil
	}
	return tagsToMap(page.Tags), nil
}
