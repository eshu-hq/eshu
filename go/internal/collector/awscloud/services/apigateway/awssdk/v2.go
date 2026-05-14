package awssdk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigatewayv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	awsapigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"

	apigatewayservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/apigateway"
)

type v2APIClient interface {
	GetApis(context.Context, *awsapigatewayv2.GetApisInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetApisOutput, error)
	GetStages(context.Context, *awsapigatewayv2.GetStagesInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetStagesOutput, error)
	GetIntegrations(context.Context, *awsapigatewayv2.GetIntegrationsInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetIntegrationsOutput, error)
	GetDomainNames(context.Context, *awsapigatewayv2.GetDomainNamesInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetDomainNamesOutput, error)
	GetApiMappings(context.Context, *awsapigatewayv2.GetApiMappingsInput, ...func(*awsapigatewayv2.Options)) (*awsapigatewayv2.GetApiMappingsOutput, error)
}

func (c *Client) listV2APIs(ctx context.Context) ([]apigatewayservice.V2API, error) {
	var apis []apigatewayservice.V2API
	var token *string
	for {
		var page *awsapigatewayv2.GetApisOutput
		err := c.recordAPICall(ctx, "GetApis", func(callCtx context.Context) error {
			var err error
			page, err = c.v2.GetApis(callCtx, &awsapigatewayv2.GetApisInput{
				MaxResults: aws.String(v2PageLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return apis, nil
		}
		for _, item := range page.Items {
			api, err := c.v2APIMetadata(ctx, item)
			if err != nil {
				return nil, err
			}
			apis = append(apis, api)
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return apis, nil
		}
	}
}

func (c *Client) v2APIMetadata(
	ctx context.Context,
	item awsapigatewayv2types.Api,
) (apigatewayservice.V2API, error) {
	api := mapV2API(item)
	var err error
	api.Stages, err = c.listV2Stages(ctx, api.ID)
	if err != nil {
		return apigatewayservice.V2API{}, err
	}
	api.Integrations, err = c.listV2Integrations(ctx, api.ID)
	if err != nil {
		return apigatewayservice.V2API{}, err
	}
	return api, nil
}

func (c *Client) listV2Stages(ctx context.Context, apiID string) ([]apigatewayservice.Stage, error) {
	var stages []apigatewayservice.Stage
	var token *string
	for {
		var page *awsapigatewayv2.GetStagesOutput
		err := c.recordAPICall(ctx, "GetStages", func(callCtx context.Context) error {
			var err error
			page, err = c.v2.GetStages(callCtx, &awsapigatewayv2.GetStagesInput{
				ApiId:      aws.String(apiID),
				MaxResults: aws.String(v2PageLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return stages, nil
		}
		for _, item := range page.Items {
			stages = append(stages, mapV2Stage(apiID, item))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return stages, nil
		}
	}
}

func (c *Client) listV2Integrations(ctx context.Context, apiID string) ([]apigatewayservice.Integration, error) {
	var integrations []apigatewayservice.Integration
	var token *string
	for {
		var page *awsapigatewayv2.GetIntegrationsOutput
		err := c.recordAPICall(ctx, "GetIntegrations", func(callCtx context.Context) error {
			var err error
			page, err = c.v2.GetIntegrations(callCtx, &awsapigatewayv2.GetIntegrationsInput{
				ApiId:      aws.String(apiID),
				MaxResults: aws.String(v2PageLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return integrations, nil
		}
		for _, item := range page.Items {
			integrations = append(integrations, mapV2Integration(apiID, item))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return integrations, nil
		}
	}
}

func (c *Client) listV2Domains(ctx context.Context) ([]apigatewayservice.DomainName, error) {
	var domains []apigatewayservice.DomainName
	var token *string
	for {
		var page *awsapigatewayv2.GetDomainNamesOutput
		err := c.recordAPICall(ctx, "GetDomainNames", func(callCtx context.Context) error {
			var err error
			page, err = c.v2.GetDomainNames(callCtx, &awsapigatewayv2.GetDomainNamesInput{
				MaxResults: aws.String(v2PageLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return domains, nil
		}
		for _, item := range page.Items {
			domain := mapV2Domain(item)
			mappings, err := c.listV2Mappings(ctx, domain.Name)
			if err != nil {
				return nil, err
			}
			domain.Mappings = mappings
			domains = append(domains, domain)
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return domains, nil
		}
	}
}

func (c *Client) listV2Mappings(ctx context.Context, domainName string) ([]apigatewayservice.Mapping, error) {
	var mappings []apigatewayservice.Mapping
	var token *string
	for {
		var page *awsapigatewayv2.GetApiMappingsOutput
		err := c.recordAPICall(ctx, "GetApiMappings", func(callCtx context.Context) error {
			var err error
			page, err = c.v2.GetApiMappings(callCtx, &awsapigatewayv2.GetApiMappingsInput{
				DomainName: aws.String(domainName),
				MaxResults: aws.String(v2PageLimit),
				NextToken:  token,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return mappings, nil
		}
		for _, item := range page.Items {
			mappings = append(mappings, mapV2Mapping(domainName, item))
		}
		token = page.NextToken
		if aws.ToString(token) == "" {
			return mappings, nil
		}
	}
}
