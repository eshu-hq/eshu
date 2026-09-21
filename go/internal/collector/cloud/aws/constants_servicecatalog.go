// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceServiceCatalog identifies the regional AWS Service Catalog
	// metadata-only scan slice. The scanner emits portfolio, product, and
	// provisioned-product configuration metadata only; provisioning-artifact
	// template bodies, launch-constraint policy documents, provisioning
	// parameter values, and any record output values are never read or
	// persisted through this service kind.
	ServiceServiceCatalog = "servicecatalog"
)

const (
	// ResourceTypeServiceCatalogPortfolio identifies an AWS Service Catalog
	// portfolio metadata resource. The scanner emits portfolio identity, ARN,
	// display name, provider name, description, and creation timestamp only.
	ResourceTypeServiceCatalogPortfolio = "aws_servicecatalog_portfolio"
	// ResourceTypeServiceCatalogProduct identifies an AWS Service Catalog
	// product metadata resource. The scanner emits product identity, ARN, name,
	// product type, owner, distributor, status, and creation timestamp only;
	// provisioning-artifact template bodies stay outside the contract.
	ResourceTypeServiceCatalogProduct = "aws_servicecatalog_product"
	// ResourceTypeServiceCatalogProvisionedProduct identifies an AWS Service
	// Catalog provisioned-product metadata resource. The scanner emits
	// provisioned-product identity, ARN, status, type, product identifier,
	// provisioning-artifact identifier, provisioning-artifact name, deployed
	// CloudFormation stack physical identifier, and creation timestamp only;
	// provisioning parameter values and stack output values stay outside the
	// contract.
	ResourceTypeServiceCatalogProvisionedProduct = "aws_servicecatalog_provisioned_product"
)

const (
	// RelationshipServiceCatalogProvisionedProductDeploysCloudFormationStack
	// records that a provisioned product of type CFN_STACK deploys a
	// CloudFormation stack, derived from the provisioned product's
	// CloudFormation stack ARN physical identifier. The edge carries the stack
	// ARN identity only; no template body is read.
	RelationshipServiceCatalogProvisionedProductDeploysCloudFormationStack = "servicecatalog_provisioned_product_deploys_cloudformation_stack"
	// RelationshipServiceCatalogProductInPortfolio records a product's reported
	// association with a portfolio, derived from ListPortfoliosForProduct.
	RelationshipServiceCatalogProductInPortfolio = "servicecatalog_product_in_portfolio"
	// RelationshipServiceCatalogPortfolioGrantsPrincipal records a portfolio's
	// reported IAM role principal association, derived from
	// ListPrincipalsForPortfolio. The edge is emitted only when AWS reports a
	// fully defined IAM role ARN; IAM_PATTERN wildcard principals that name no
	// concrete role node stay outside the contract.
	RelationshipServiceCatalogPortfolioGrantsPrincipal = "servicecatalog_portfolio_grants_principal"
)
