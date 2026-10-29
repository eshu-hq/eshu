// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigateway "github.com/aws/aws-sdk-go-v2/service/apigateway"
	awsapigatewaytypes "github.com/aws/aws-sdk-go-v2/service/apigateway/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	apigatewayservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/service/apigateway"
)

type restAPIClient interface {
	GetRestApis(context.Context, *awsapigateway.GetRestApisInput, ...func(*awsapigateway.Options)) (*awsapigateway.GetRestApisOutput, error)
	GetStages(context.Context, *awsapigateway.GetStagesInput, ...func(*awsapigateway.Options)) (*awsapigateway.GetStagesOutput, error)
	GetResources(context.Context, *awsapigateway.GetResourcesInput, ...func(*awsapigateway.Options)) (*awsapigateway.GetResourcesOutput, error)
	GetDomainNames(context.Context, *awsapigateway.GetDomainNamesInput, ...func(*awsapigateway.Options)) (*awsapigateway.GetDomainNamesOutput, error)
	GetBasePathMappings(context.Context, *awsapigateway.GetBasePathMappingsInput, ...func(*awsapigateway.Options)) (*awsapigateway.GetBasePathMappingsOutput, error)
}

func (c *Client) listRESTAPIs(
	ctx context.Context,
) ([]apigatewayservice.RESTAPI, []awscloud.WarningObservation, error) {
	var apis []apigatewayservice.RESTAPI
	var warnings []awscloud.WarningObservation
	var position *string
	for {
		var page *awsapigateway.GetRestApisOutput
		err := c.recordAPICall(ctx, "GetRestApis", func(callCtx context.Context) error {
			var err error
			page, err = c.rest.GetRestApis(callCtx, &awsapigateway.GetRestApisInput{
				Limit:    aws.Int32(restPageLimit),
				Position: position,
			})
			return err
		})
		if isNetworkTimeoutError(err) {
			return nil, []awscloud.WarningObservation{c.networkTimeoutWarning(
				"GetRestApis", apigatewayservice.APIKindREST, "rest_apis",
				"REST API inventory", "apigateway_rest_apis_timeout",
			)}, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if page == nil {
			return apis, warnings, nil
		}
		for _, item := range page.Items {
			api, apiWarnings, err := c.restAPIMetadata(ctx, item)
			if err != nil {
				return nil, nil, err
			}
			warnings = appendWarningOnce(warnings, apiWarnings...)
			apis = append(apis, api)
		}
		position = page.Position
		if aws.ToString(position) == "" {
			return apis, warnings, nil
		}
	}
}

func (c *Client) restAPIMetadata(
	ctx context.Context,
	item awsapigatewaytypes.RestApi,
) (apigatewayservice.RESTAPI, []awscloud.WarningObservation, error) {
	api := mapRESTAPI(item)
	var warnings []awscloud.WarningObservation
	var err error
	api.Stages, warnings, err = c.listRESTStages(ctx, api.ID)
	if err != nil {
		return apigatewayservice.RESTAPI{}, nil, err
	}
	var integrationWarnings []awscloud.WarningObservation
	api.Integrations, integrationWarnings, err = c.listRESTIntegrations(ctx, api.ID)
	if err != nil {
		return apigatewayservice.RESTAPI{}, nil, err
	}
	warnings = appendWarningOnce(warnings, integrationWarnings...)
	return api, warnings, nil
}

func (c *Client) listRESTStages(
	ctx context.Context,
	apiID string,
) ([]apigatewayservice.Stage, []awscloud.WarningObservation, error) {
	var page *awsapigateway.GetStagesOutput
	err := c.recordAPICall(ctx, "GetStages", func(callCtx context.Context) error {
		var err error
		page, err = c.rest.GetStages(callCtx, &awsapigateway.GetStagesInput{RestApiId: aws.String(apiID)})
		return err
	})
	if isNetworkTimeoutError(err) {
		return nil, []awscloud.WarningObservation{c.networkTimeoutWarning(
			"GetStages", apigatewayservice.APIKindREST, "rest_stages",
			"REST stage inventory", "apigateway_rest_stages_timeout",
		)}, nil
	}
	if err != nil || page == nil {
		return nil, nil, err
	}
	stages := make([]apigatewayservice.Stage, 0, len(page.Item))
	for _, item := range page.Item {
		stages = append(stages, mapRESTStage(apiID, item))
	}
	return stages, nil, nil
}

func (c *Client) listRESTIntegrations(
	ctx context.Context,
	apiID string,
) ([]apigatewayservice.Integration, []awscloud.WarningObservation, error) {
	var integrations []apigatewayservice.Integration
	var position *string
	for {
		var page *awsapigateway.GetResourcesOutput
		err := c.recordAPICall(ctx, "GetResources", func(callCtx context.Context) error {
			var err error
			page, err = c.rest.GetResources(callCtx, &awsapigateway.GetResourcesInput{
				RestApiId: aws.String(apiID),
				Embed:     []string{"methods"},
				Limit:     aws.Int32(restPageLimit),
				Position:  position,
			})
			return err
		})
		if isThrottleError(err) {
			return nil, []awscloud.WarningObservation{c.restResourcesThrottleWarning()}, nil
		}
		if isNetworkTimeoutError(err) {
			return nil, []awscloud.WarningObservation{c.networkTimeoutWarning(
				"GetResources", apigatewayservice.APIKindREST, "rest_integrations",
				"REST integration metadata", "apigateway_rest_integrations_timeout",
			)}, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if page == nil {
			return integrations, nil, nil
		}
		for _, resource := range page.Items {
			integrations = append(integrations, mapRESTResourceIntegrations(apiID, resource)...)
		}
		position = page.Position
		if aws.ToString(position) == "" {
			return integrations, nil, nil
		}
	}
}

// appendWarningOnce merges per-call degrade warnings into the scan warning
// set, keeping one observation per (warning kind, error class, operation).
// Every warning produced in this package carries those three keys, so the
// throttle behavior is unchanged (one GetResources throttle warning per scan)
// while each timed-out operation is still recorded exactly once.
func appendWarningOnce(
	warnings []awscloud.WarningObservation,
	candidates ...awscloud.WarningObservation,
) []awscloud.WarningObservation {
	for _, candidate := range candidates {
		key := warningDedupeKey(candidate)
		seen := false
		for _, warning := range warnings {
			if warningDedupeKey(warning) == key {
				seen = true
				break
			}
		}
		if !seen {
			warnings = append(warnings, candidate)
		}
	}
	return warnings
}

func warningDedupeKey(warning awscloud.WarningObservation) string {
	operation, _ := warning.Attributes["operation"].(string)
	return warning.WarningKind + "\x00" + warning.ErrorClass + "\x00" + operation
}

func (c *Client) restResourcesThrottleWarning() awscloud.WarningObservation {
	return awscloud.WarningObservation{
		Boundary:    c.boundary,
		WarningKind: awscloud.WarningThrottleSustained,
		ErrorClass:  "throttled",
		Message:     "API Gateway GetResources throttled after SDK retries; REST integration metadata omitted for this scan",
		Attributes: map[string]any{
			"api_kind":          apigatewayservice.APIKindREST,
			"operation":         "GetResources",
			"partial_component": "rest_integrations",
		},
		SourceRecordID: "apigateway_rest_integrations_throttled",
	}
}

func (c *Client) listRESTDomains(
	ctx context.Context,
) ([]apigatewayservice.DomainName, []awscloud.WarningObservation, error) {
	var domains []apigatewayservice.DomainName
	var warnings []awscloud.WarningObservation
	var position *string
	for {
		var page *awsapigateway.GetDomainNamesOutput
		err := c.recordAPICall(ctx, "GetDomainNames", func(callCtx context.Context) error {
			var err error
			page, err = c.rest.GetDomainNames(callCtx, &awsapigateway.GetDomainNamesInput{
				Limit:    aws.Int32(restPageLimit),
				Position: position,
			})
			return err
		})
		if isNetworkTimeoutError(err) {
			return nil, []awscloud.WarningObservation{c.networkTimeoutWarning(
				"GetDomainNames", apigatewayservice.APIKindREST, "rest_domains",
				"REST custom-domain inventory", "apigateway_rest_domains_timeout",
			)}, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if page == nil {
			return domains, warnings, nil
		}
		for _, item := range page.Items {
			domain := mapRESTDomain(item)
			mappings, mappingWarnings, err := c.listRESTMappings(ctx, domain.Name)
			if err != nil {
				return nil, nil, err
			}
			warnings = appendWarningOnce(warnings, mappingWarnings...)
			domain.Mappings = mappings
			domains = append(domains, domain)
		}
		position = page.Position
		if aws.ToString(position) == "" {
			return domains, warnings, nil
		}
	}
}

func (c *Client) listRESTMappings(
	ctx context.Context,
	domainName string,
) ([]apigatewayservice.Mapping, []awscloud.WarningObservation, error) {
	var mappings []apigatewayservice.Mapping
	var position *string
	for {
		var page *awsapigateway.GetBasePathMappingsOutput
		err := c.recordAPICall(ctx, "GetBasePathMappings", func(callCtx context.Context) error {
			var err error
			page, err = c.rest.GetBasePathMappings(callCtx, &awsapigateway.GetBasePathMappingsInput{
				DomainName: aws.String(domainName),
				Limit:      aws.Int32(restPageLimit),
				Position:   position,
			})
			return err
		})
		if isNetworkTimeoutError(err) {
			return nil, []awscloud.WarningObservation{c.networkTimeoutWarning(
				"GetBasePathMappings", apigatewayservice.APIKindREST, "rest_mappings",
				"REST domain-mapping inventory", "apigateway_rest_mappings_timeout",
			)}, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if page == nil {
			return mappings, nil, nil
		}
		for _, item := range page.Items {
			mappings = append(mappings, mapRESTMapping(domainName, item))
		}
		position = page.Position
		if aws.ToString(position) == "" {
			return mappings, nil, nil
		}
	}
}
