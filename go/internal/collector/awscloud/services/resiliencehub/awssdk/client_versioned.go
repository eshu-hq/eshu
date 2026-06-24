// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsresiliencehub "github.com/aws/aws-sdk-go-v2/service/resiliencehub"
	awsresiliencehubtypes "github.com/aws/aws-sdk-go-v2/service/resiliencehub/types"

	resiliencehubservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/resiliencehub"
)

// listInputSources pages every published-version input source for one app.
func (c *Client) listInputSources(ctx context.Context, appARN string) ([]resiliencehubservice.InputSource, error) {
	var sources []resiliencehubservice.InputSource
	var nextToken *string
	for {
		var page *awsresiliencehub.ListAppInputSourcesOutput
		err := c.recordAPICall(ctx, "ListAppInputSources", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListAppInputSources(callCtx, &awsresiliencehub.ListAppInputSourcesInput{
				AppArn:     aws.String(appARN),
				AppVersion: aws.String(publishedAppVersion),
				NextToken:  nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return sources, nil
		}
		for _, source := range page.AppInputSources {
			sources = append(sources, mapInputSource(source))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return sources, nil
		}
	}
}

func mapInputSource(source awsresiliencehubtypes.AppInputSource) resiliencehubservice.InputSource {
	return resiliencehubservice.InputSource{
		ImportType:    strings.TrimSpace(string(source.ImportType)),
		SourceName:    strings.TrimSpace(aws.ToString(source.SourceName)),
		SourceARN:     strings.TrimSpace(aws.ToString(source.SourceArn)),
		ResourceCount: source.ResourceCount,
	}
}

// listComponents pages every published-version application component for one app.
func (c *Client) listComponents(ctx context.Context, appARN string) ([]resiliencehubservice.AppComponent, error) {
	var components []resiliencehubservice.AppComponent
	var nextToken *string
	for {
		var page *awsresiliencehub.ListAppVersionAppComponentsOutput
		err := c.recordAPICall(ctx, "ListAppVersionAppComponents", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListAppVersionAppComponents(callCtx, &awsresiliencehub.ListAppVersionAppComponentsInput{
				AppArn:     aws.String(appARN),
				AppVersion: aws.String(publishedAppVersion),
				NextToken:  nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return components, nil
		}
		for _, component := range page.AppComponents {
			components = append(components, mapComponent(component))
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return components, nil
		}
	}
}

func mapComponent(component awsresiliencehubtypes.AppComponent) resiliencehubservice.AppComponent {
	return resiliencehubservice.AppComponent{
		Name: strings.TrimSpace(aws.ToString(component.Name)),
		Type: strings.TrimSpace(aws.ToString(component.Type)),
	}
}

// listProtectedResources pages every published-version physical resource for one
// app and keeps only those Resilience Hub identifies by an ARN, so the
// scanner-owned model never carries a native (non-ARN) identifier the
// protected-resource edge could not join.
func (c *Client) listProtectedResources(ctx context.Context, appARN string) ([]resiliencehubservice.ProtectedResource, error) {
	var resources []resiliencehubservice.ProtectedResource
	var nextToken *string
	for {
		var page *awsresiliencehub.ListAppVersionResourcesOutput
		err := c.recordAPICall(ctx, "ListAppVersionResources", func(callCtx context.Context) error {
			var callErr error
			page, callErr = c.client.ListAppVersionResources(callCtx, &awsresiliencehub.ListAppVersionResourcesInput{
				AppArn:     aws.String(appARN),
				AppVersion: aws.String(publishedAppVersion),
				NextToken:  nextToken,
			})
			return callErr
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return resources, nil
		}
		for _, resource := range page.PhysicalResources {
			if mapped, ok := mapProtectedResource(resource); ok {
				resources = append(resources, mapped)
			}
		}
		nextToken = page.NextToken
		if aws.ToString(nextToken) == "" {
			return resources, nil
		}
	}
}

// mapProtectedResource keeps a physical resource only when Resilience Hub
// reports its identifier as an ARN. Native (non-ARN) identifiers are dropped so
// the scanner never emits a protected-resource edge it cannot join.
func mapProtectedResource(resource awsresiliencehubtypes.PhysicalResource) (resiliencehubservice.ProtectedResource, bool) {
	id := resource.PhysicalResourceId
	if id == nil || id.Type != awsresiliencehubtypes.PhysicalIdentifierTypeArn {
		return resiliencehubservice.ProtectedResource{}, false
	}
	arn := strings.TrimSpace(aws.ToString(id.Identifier))
	if arn == "" {
		return resiliencehubservice.ProtectedResource{}, false
	}
	logicalID := ""
	if resource.LogicalResourceId != nil {
		logicalID = strings.TrimSpace(aws.ToString(resource.LogicalResourceId.Identifier))
	}
	return resiliencehubservice.ProtectedResource{
		ARN:               arn,
		ResilienceHubType: strings.TrimSpace(aws.ToString(resource.ResourceType)),
		LogicalResourceID: logicalID,
	}, true
}
