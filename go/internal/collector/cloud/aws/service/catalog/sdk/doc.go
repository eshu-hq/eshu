// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Service Catalog client into the
// metadata-only Service Catalog scanner interface.
//
// The adapter uses ListPortfolios, SearchProductsAsAdmin, ScanProvisionedProducts,
// SearchProvisionedProducts (to resolve the deployed CloudFormation stack ARN
// physical identifier), ListPortfoliosForProduct, and ListPrincipalsForPortfolio.
// It intentionally excludes ProvisionProduct, UpdateProvisionedProduct,
// TerminateProvisionedProduct, CreateProduct, UpdateProduct, DeleteProduct,
// CreatePortfolio, DeletePortfolio, AssociatePrincipalWithPortfolio,
// AssociateProductWithPortfolio, CreateConstraint, DescribeProvisioningArtifact
// template reads, DescribeRecord output reads, and every other mutation,
// association, constraint, or sensitive-payload API.
package awssdk
