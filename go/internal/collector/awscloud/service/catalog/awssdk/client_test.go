// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssc "github.com/aws/aws-sdk-go-v2/service/servicecatalog"
	awssctypes "github.com/aws/aws-sdk-go-v2/service/servicecatalog/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceServiceCatalog,
		ScopeID:             "scope-1",
		GenerationID:        "gen-1",
		CollectorInstanceID: "collector-aws-1",
		FencingToken:        1,
		ObservedAt:          time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
	}
}

// fakeServiceCatalogAPI is a single-page fake of the SDK surface the adapter
// consumes. Each method returns the configured output and records that it was
// called so tests can assert the metadata-only API set is exercised.
type fakeServiceCatalogAPI struct {
	portfolios       *awssc.ListPortfoliosOutput
	products         *awssc.SearchProductsAsAdminOutput
	scanned          *awssc.ScanProvisionedProductsOutput
	searched         *awssc.SearchProvisionedProductsOutput
	portfoliosByProd *awssc.ListPortfoliosForProductOutput
	principals       *awssc.ListPrincipalsForPortfolioOutput
}

func (f *fakeServiceCatalogAPI) ListPortfolios(context.Context, *awssc.ListPortfoliosInput, ...func(*awssc.Options)) (*awssc.ListPortfoliosOutput, error) {
	return f.portfolios, nil
}

func (f *fakeServiceCatalogAPI) SearchProductsAsAdmin(context.Context, *awssc.SearchProductsAsAdminInput, ...func(*awssc.Options)) (*awssc.SearchProductsAsAdminOutput, error) {
	return f.products, nil
}

func (f *fakeServiceCatalogAPI) ScanProvisionedProducts(context.Context, *awssc.ScanProvisionedProductsInput, ...func(*awssc.Options)) (*awssc.ScanProvisionedProductsOutput, error) {
	return f.scanned, nil
}

func (f *fakeServiceCatalogAPI) SearchProvisionedProducts(context.Context, *awssc.SearchProvisionedProductsInput, ...func(*awssc.Options)) (*awssc.SearchProvisionedProductsOutput, error) {
	return f.searched, nil
}

func (f *fakeServiceCatalogAPI) ListPortfoliosForProduct(context.Context, *awssc.ListPortfoliosForProductInput, ...func(*awssc.Options)) (*awssc.ListPortfoliosForProductOutput, error) {
	return f.portfoliosByProd, nil
}

func (f *fakeServiceCatalogAPI) ListPrincipalsForPortfolio(context.Context, *awssc.ListPrincipalsForPortfolioInput, ...func(*awssc.Options)) (*awssc.ListPrincipalsForPortfolioOutput, error) {
	return f.principals, nil
}

func TestClientListPortfoliosMapsSafeMetadata(t *testing.T) {
	adapter := &Client{
		client: &fakeServiceCatalogAPI{portfolios: &awssc.ListPortfoliosOutput{
			PortfolioDetails: []awssctypes.PortfolioDetail{{
				Id:           aws.String("port-abc123"),
				ARN:          aws.String("arn:aws:catalog:us-east-1:123456789012:portfolio/port-abc123"),
				DisplayName:  aws.String("Platform Portfolio"),
				ProviderName: aws.String("Platform Team"),
				Description:  aws.String("Shared products"),
				CreatedTime:  aws.Time(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
			}},
		}},
		boundary: testBoundary(),
	}
	portfolios, err := adapter.ListPortfolios(context.Background())
	if err != nil {
		t.Fatalf("ListPortfolios() error = %v", err)
	}
	if len(portfolios) != 1 {
		t.Fatalf("len(portfolios) = %d, want 1", len(portfolios))
	}
	if portfolios[0].ARN != "arn:aws:catalog:us-east-1:123456789012:portfolio/port-abc123" {
		t.Fatalf("portfolio ARN = %q", portfolios[0].ARN)
	}
	if portfolios[0].DisplayName != "Platform Portfolio" {
		t.Fatalf("portfolio DisplayName = %q", portfolios[0].DisplayName)
	}
}

func TestClientListProductsReadsSummary(t *testing.T) {
	adapter := &Client{
		client: &fakeServiceCatalogAPI{products: &awssc.SearchProductsAsAdminOutput{
			ProductViewDetails: []awssctypes.ProductViewDetail{{
				ProductARN: aws.String("arn:aws:catalog:us-east-1:123456789012:product/prod-xyz789"),
				Status:     awssctypes.StatusAvailable,
				ProductViewSummary: &awssctypes.ProductViewSummary{
					ProductId: aws.String("prod-xyz789"),
					Name:      aws.String("Bucket Product"),
					Owner:     aws.String("Platform Team"),
					Type:      awssctypes.ProductTypeCloudFormationTemplate,
				},
			}},
		}},
		boundary: testBoundary(),
	}
	products, err := adapter.ListProducts(context.Background())
	if err != nil {
		t.Fatalf("ListProducts() error = %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("len(products) = %d, want 1", len(products))
	}
	if products[0].ID != "prod-xyz789" {
		t.Fatalf("product ID = %q", products[0].ID)
	}
	if products[0].ProductType != string(awssctypes.ProductTypeCloudFormationTemplate) {
		t.Fatalf("product type = %q", products[0].ProductType)
	}
	if products[0].Owner != "Platform Team" {
		t.Fatalf("product owner = %q", products[0].Owner)
	}
}

// TestClientListProvisionedProductsStampsPhysicalID confirms the adapter
// resolves the CloudFormation stack ARN physical identifier from the
// SearchProvisionedProducts index and stamps it onto the
// ScanProvisionedProducts detail, which omits the physical identifier.
func TestClientListProvisionedProductsStampsPhysicalID(t *testing.T) {
	adapter := &Client{
		client: &fakeServiceCatalogAPI{
			scanned: &awssc.ScanProvisionedProductsOutput{
				ProvisionedProducts: []awssctypes.ProvisionedProductDetail{{
					Id:        aws.String("pp-stack001"),
					Arn:       aws.String("arn:aws:servicecatalog:us-east-1:123456789012:stack/team/pp-stack001"),
					Name:      aws.String("team-bucket"),
					Status:    awssctypes.ProvisionedProductStatusAvailable,
					Type:      aws.String("CFN_STACK"),
					ProductId: aws.String("prod-xyz789"),
				}},
			},
			searched: &awssc.SearchProvisionedProductsOutput{
				ProvisionedProducts: []awssctypes.ProvisionedProductAttribute{{
					Id:         aws.String("pp-stack001"),
					PhysicalId: aws.String("arn:aws:cloudformation:us-east-1:123456789012:stack/SC-team/abcd-1234"),
					Type:       aws.String("CFN_STACK"),
				}},
			},
		},
		boundary: testBoundary(),
	}
	provisioned, err := adapter.ListProvisionedProducts(context.Background())
	if err != nil {
		t.Fatalf("ListProvisionedProducts() error = %v", err)
	}
	if len(provisioned) != 1 {
		t.Fatalf("len(provisioned) = %d, want 1", len(provisioned))
	}
	want := "arn:aws:cloudformation:us-east-1:123456789012:stack/SC-team/abcd-1234"
	if provisioned[0].PhysicalID != want {
		t.Fatalf("provisioned PhysicalID = %q, want %q", provisioned[0].PhysicalID, want)
	}
	if provisioned[0].Type != "CFN_STACK" {
		t.Fatalf("provisioned Type = %q, want CFN_STACK", provisioned[0].Type)
	}
}

func TestClientPrincipalsForPortfolioMapsARNAndType(t *testing.T) {
	adapter := &Client{
		client: &fakeServiceCatalogAPI{principals: &awssc.ListPrincipalsForPortfolioOutput{
			Principals: []awssctypes.Principal{{
				PrincipalARN:  aws.String("arn:aws:iam::123456789012:role/LaunchRole"),
				PrincipalType: awssctypes.PrincipalTypeIam,
			}},
		}},
		boundary: testBoundary(),
	}
	principals, err := adapter.PrincipalsForPortfolio(context.Background(), "port-abc123")
	if err != nil {
		t.Fatalf("PrincipalsForPortfolio() error = %v", err)
	}
	if len(principals) != 1 {
		t.Fatalf("len(principals) = %d, want 1", len(principals))
	}
	if principals[0].ARN != "arn:aws:iam::123456789012:role/LaunchRole" {
		t.Fatalf("principal ARN = %q", principals[0].ARN)
	}
	if principals[0].Type != string(awssctypes.PrincipalTypeIam) {
		t.Fatalf("principal type = %q", principals[0].Type)
	}
}

func TestClientPrincipalsForPortfolioSkipsBlankID(t *testing.T) {
	adapter := &Client{client: &fakeServiceCatalogAPI{}, boundary: testBoundary()}
	principals, err := adapter.PrincipalsForPortfolio(context.Background(), "  ")
	if err != nil {
		t.Fatalf("PrincipalsForPortfolio() error = %v", err)
	}
	if principals != nil {
		t.Fatalf("PrincipalsForPortfolio() = %v, want nil for blank id", principals)
	}
}
