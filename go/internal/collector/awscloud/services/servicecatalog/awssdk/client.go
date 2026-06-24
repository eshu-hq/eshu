// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssc "github.com/aws/aws-sdk-go-v2/service/servicecatalog"
	awssctypes "github.com/aws/aws-sdk-go-v2/service/servicecatalog/types"
	"github.com/aws/smithy-go"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	scservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/servicecatalog"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// accessLevelSelf scopes provisioned-product scans to the whole account
// (Account access level with the only supported value self) so the scanner sees
// every provisioned product the claim can read, not just the caller's own.
var accessLevelSelf = &awssctypes.AccessLevelFilter{
	Key:   awssctypes.AccessLevelFilterKeyAccount,
	Value: aws.String("self"),
}

type apiClient interface {
	ListPortfolios(context.Context, *awssc.ListPortfoliosInput, ...func(*awssc.Options)) (*awssc.ListPortfoliosOutput, error)
	SearchProductsAsAdmin(context.Context, *awssc.SearchProductsAsAdminInput, ...func(*awssc.Options)) (*awssc.SearchProductsAsAdminOutput, error)
	ScanProvisionedProducts(context.Context, *awssc.ScanProvisionedProductsInput, ...func(*awssc.Options)) (*awssc.ScanProvisionedProductsOutput, error)
	SearchProvisionedProducts(context.Context, *awssc.SearchProvisionedProductsInput, ...func(*awssc.Options)) (*awssc.SearchProvisionedProductsOutput, error)
	ListPortfoliosForProduct(context.Context, *awssc.ListPortfoliosForProductInput, ...func(*awssc.Options)) (*awssc.ListPortfoliosForProductOutput, error)
	ListPrincipalsForPortfolio(context.Context, *awssc.ListPrincipalsForPortfolioInput, ...func(*awssc.Options)) (*awssc.ListPrincipalsForPortfolioOutput, error)
}

// Client adapts AWS SDK Service Catalog pagination into scanner-owned metadata.
// The adapter never provisions, updates, or terminates products, never
// associates or disassociates principals or portfolios, never mutates
// constraints, and never reads provisioning-artifact template bodies, launch
// constraint policy documents, or record output values.
type Client struct {
	client      apiClient
	boundary    awscloud.Boundary
	tracer      trace.Tracer
	instruments *telemetry.Instruments
}

// NewClient builds a Service Catalog SDK adapter for one claimed AWS boundary.
func NewClient(
	config aws.Config,
	boundary awscloud.Boundary,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
) *Client {
	return &Client{
		client:      awssc.NewFromConfig(config),
		boundary:    boundary,
		tracer:      tracer,
		instruments: instruments,
	}
}

// ListPortfolios reads Service Catalog portfolio metadata across paginated
// ListPortfolios responses.
func (c *Client) ListPortfolios(ctx context.Context) ([]scservice.Portfolio, error) {
	var portfolios []scservice.Portfolio
	var pageToken *string
	for {
		var page *awssc.ListPortfoliosOutput
		err := c.recordAPICall(ctx, "ListPortfolios", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListPortfolios(callCtx, &awssc.ListPortfoliosInput{
				PageToken: pageToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return portfolios, nil
		}
		for _, detail := range page.PortfolioDetails {
			portfolios = append(portfolios, mapPortfolio(detail))
		}
		pageToken = page.NextPageToken
		if aws.ToString(pageToken) == "" {
			return portfolios, nil
		}
	}
}

// ListProducts reads Service Catalog product metadata across paginated
// SearchProductsAsAdmin responses. Only safe identity, type, and ownership
// fields survive; provisioning-artifact template bodies are never requested.
func (c *Client) ListProducts(ctx context.Context) ([]scservice.Product, error) {
	var products []scservice.Product
	var pageToken *string
	for {
		var page *awssc.SearchProductsAsAdminOutput
		err := c.recordAPICall(ctx, "SearchProductsAsAdmin", func(callCtx context.Context) error {
			var err error
			page, err = c.client.SearchProductsAsAdmin(callCtx, &awssc.SearchProductsAsAdminInput{
				PageToken: pageToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return products, nil
		}
		for _, detail := range page.ProductViewDetails {
			products = append(products, mapProduct(detail))
		}
		pageToken = page.NextPageToken
		if aws.ToString(pageToken) == "" {
			return products, nil
		}
	}
}

// ListProvisionedProducts reads Service Catalog provisioned-product metadata via
// ScanProvisionedProducts (account scope) and stamps each detail with its
// deployed CloudFormation stack ARN physical identifier resolved from the
// SearchProvisionedProducts index. ScanProvisionedProducts omits the physical
// identifier, so the index supplies the graph-join anchor without reading any
// stack template body or record output value.
func (c *Client) ListProvisionedProducts(ctx context.Context) ([]scservice.ProvisionedProduct, error) {
	physicalIDs, err := c.provisionedProductPhysicalIDs(ctx)
	if err != nil {
		return nil, err
	}
	var provisioned []scservice.ProvisionedProduct
	var pageToken *string
	for {
		var page *awssc.ScanProvisionedProductsOutput
		err := c.recordAPICall(ctx, "ScanProvisionedProducts", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ScanProvisionedProducts(callCtx, &awssc.ScanProvisionedProductsInput{
				AccessLevelFilter: accessLevelSelf,
				PageToken:         pageToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return provisioned, nil
		}
		for _, detail := range page.ProvisionedProducts {
			item := mapProvisionedProduct(detail)
			if physicalID, ok := physicalIDs[item.ID]; ok && item.PhysicalID == "" {
				item.PhysicalID = physicalID
			}
			provisioned = append(provisioned, item)
		}
		pageToken = page.NextPageToken
		if aws.ToString(pageToken) == "" {
			return provisioned, nil
		}
	}
}

// provisionedProductPhysicalIDs builds a provisioned-product-id -> physical-id
// index from SearchProvisionedProducts. The physical id of a CFN_STACK
// provisioned product is its CloudFormation stack ARN, which ScanProvisionedProducts
// does not return; the index is the metadata-only source for the
// provisioned-product-to-stack graph edge.
func (c *Client) provisionedProductPhysicalIDs(ctx context.Context) (map[string]string, error) {
	index := map[string]string{}
	var pageToken *string
	for {
		var page *awssc.SearchProvisionedProductsOutput
		err := c.recordAPICall(ctx, "SearchProvisionedProducts", func(callCtx context.Context) error {
			var err error
			page, err = c.client.SearchProvisionedProducts(callCtx, &awssc.SearchProvisionedProductsInput{
				AccessLevelFilter: accessLevelSelf,
				PageToken:         pageToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return index, nil
		}
		for _, attribute := range page.ProvisionedProducts {
			id := strings.TrimSpace(aws.ToString(attribute.Id))
			physicalID := strings.TrimSpace(aws.ToString(attribute.PhysicalId))
			if id != "" && physicalID != "" {
				index[id] = physicalID
			}
		}
		pageToken = page.NextPageToken
		if aws.ToString(pageToken) == "" {
			return index, nil
		}
	}
}

// PortfoliosForProduct reads the portfolios a product is associated with across
// paginated ListPortfoliosForProduct responses.
func (c *Client) PortfoliosForProduct(ctx context.Context, productID string) ([]scservice.Portfolio, error) {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return nil, nil
	}
	var portfolios []scservice.Portfolio
	var pageToken *string
	for {
		var page *awssc.ListPortfoliosForProductOutput
		err := c.recordAPICall(ctx, "ListPortfoliosForProduct", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListPortfoliosForProduct(callCtx, &awssc.ListPortfoliosForProductInput{
				ProductId: aws.String(productID),
				PageToken: pageToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return portfolios, nil
		}
		for _, detail := range page.PortfolioDetails {
			portfolios = append(portfolios, mapPortfolio(detail))
		}
		pageToken = page.NextPageToken
		if aws.ToString(pageToken) == "" {
			return portfolios, nil
		}
	}
}

// PrincipalsForPortfolio reads the IAM principals associated with a portfolio
// across paginated ListPrincipalsForPortfolio responses.
func (c *Client) PrincipalsForPortfolio(ctx context.Context, portfolioID string) ([]scservice.Principal, error) {
	portfolioID = strings.TrimSpace(portfolioID)
	if portfolioID == "" {
		return nil, nil
	}
	var principals []scservice.Principal
	var pageToken *string
	for {
		var page *awssc.ListPrincipalsForPortfolioOutput
		err := c.recordAPICall(ctx, "ListPrincipalsForPortfolio", func(callCtx context.Context) error {
			var err error
			page, err = c.client.ListPrincipalsForPortfolio(callCtx, &awssc.ListPrincipalsForPortfolioInput{
				PortfolioId: aws.String(portfolioID),
				PageToken:   pageToken,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		if page == nil {
			return principals, nil
		}
		for _, principal := range page.Principals {
			principals = append(principals, mapPrincipal(principal))
		}
		pageToken = page.NextPageToken
		if aws.ToString(pageToken) == "" {
			return principals, nil
		}
	}
}

func (c *Client) recordAPICall(ctx context.Context, operation string, call func(context.Context) error) error {
	if c.tracer != nil {
		var span trace.Span
		ctx, span = c.tracer.Start(ctx, telemetry.SpanAWSServicePaginationPage)
		span.SetAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
		)
		defer span.End()
	}
	err := call(ctx)
	result := "success"
	if err != nil {
		result = "error"
	}
	throttled := isThrottleError(err)
	awscloud.RecordAPICall(ctx, awscloud.APICallEvent{
		Boundary:  c.boundary,
		Operation: operation,
		Result:    result,
		Throttled: throttled,
	})
	if c.instruments != nil {
		c.instruments.AWSAPICalls.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrService(c.boundary.ServiceKind),
			telemetry.AttrAccount(c.boundary.AccountID),
			telemetry.AttrRegion(c.boundary.Region),
			telemetry.AttrOperation(operation),
			telemetry.AttrResult(result),
		))
		if throttled {
			c.instruments.AWSThrottles.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrService(c.boundary.ServiceKind),
				telemetry.AttrAccount(c.boundary.AccountID),
				telemetry.AttrRegion(c.boundary.Region),
			))
		}
	}
	return err
}

func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException"
}

var _ scservice.Client = (*Client)(nil)

var _ apiClient = (*awssc.Client)(nil)
