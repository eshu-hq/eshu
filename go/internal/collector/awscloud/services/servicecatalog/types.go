// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"
	"time"
)

// Client lists metadata-only AWS Service Catalog observations for one claimed
// account and region. Every method maps to a read-only AWS Service Catalog API.
// The interface deliberately excludes every provisioning, association, and
// constraint mutation API, plus the sensitive-payload read APIs that return
// provisioning-artifact template bodies, launch-constraint policy documents,
// provisioning parameter values, or record output values, so the metadata-only
// contract is enforced by construction. Read-only association lookups
// (PortfoliosForProduct, PrincipalsForPortfolio) are kept because they return
// only identity references used to derive relationship edges, not sensitive
// payloads. See TestClientInterfaceExcludesMutationAPIs.
type Client interface {
	// ListPortfolios reads Service Catalog portfolio metadata.
	ListPortfolios(ctx context.Context) ([]Portfolio, error)
	// ListProducts reads Service Catalog product metadata as administrator.
	ListProducts(ctx context.Context) ([]Product, error)
	// ListProvisionedProducts reads Service Catalog provisioned-product
	// metadata across the account scope.
	ListProvisionedProducts(ctx context.Context) ([]ProvisionedProduct, error)
	// PortfoliosForProduct reads the portfolios a product is associated with.
	PortfoliosForProduct(ctx context.Context, productID string) ([]Portfolio, error)
	// PrincipalsForPortfolio reads the IAM principals associated with a
	// portfolio.
	PrincipalsForPortfolio(ctx context.Context, portfolioID string) ([]Principal, error)
}

// Portfolio is the scanner-owned Service Catalog portfolio view. It carries
// safe identity, ownership, and timestamp metadata only.
type Portfolio struct {
	ID           string
	ARN          string
	DisplayName  string
	ProviderName string
	Description  string
	CreatedTime  time.Time
}

// Product is the scanner-owned Service Catalog product view. It carries safe
// identity, type, and ownership metadata. Provisioning-artifact template
// bodies, support URLs, and launch constraints stay outside this view.
type Product struct {
	ID          string
	ARN         string
	Name        string
	ProductType string
	Owner       string
	Distributor string
	Status      string
	CreatedTime time.Time
}

// ProvisionedProduct is the scanner-owned Service Catalog provisioned-product
// view. It carries safe identity, status, product linkage, and the deployed
// CloudFormation stack physical identifier. Provisioning parameter values and
// record output values stay outside this view.
type ProvisionedProduct struct {
	ID                       string
	ARN                      string
	Name                     string
	Status                   string
	Type                     string
	ProductID                string
	ProvisioningArtifactID   string
	ProvisioningArtifactName string
	PhysicalID               string
	CreatedTime              time.Time
}

// Principal is the scanner-owned Service Catalog portfolio-principal view. It
// carries the principal ARN and AWS-reported principal type only.
type Principal struct {
	ARN  string
	Type string
}
