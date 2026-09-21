// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Service Catalog metadata-only facts for one claimed account
// and region. It never provisions, updates, or terminates a product, never
// associates or disassociates principals or portfolios, never mutates
// constraints, and never reads provisioning-artifact template bodies, launch
// constraint policy documents, provisioning parameter values, or record output
// values. The forbidden APIs are excluded from the Client interface by
// construction; see TestClientInterfaceExcludesMutationAPIs.
type Scanner struct {
	Client Client
}

// Scan observes Service Catalog portfolios, products, and provisioned products
// through the configured client and returns reported-confidence AWS facts. The
// scan only reaches read-only list-and-describe paths and never reaches
// provisioning, association, constraint-mutation, or template-read surfaces.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("servicecatalog scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceServiceCatalog:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceServiceCatalog
	default:
		return nil, fmt.Errorf("servicecatalog scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	portfolios, err := s.Client.ListPortfolios(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Service Catalog portfolios: %w", err)
	}
	for _, portfolio := range portfolios {
		envelopes, err = s.appendPortfolio(ctx, envelopes, boundary, portfolio)
		if err != nil {
			return nil, err
		}
	}

	products, err := s.Client.ListProducts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Service Catalog products: %w", err)
	}
	for _, product := range products {
		envelopes, err = s.appendProduct(ctx, envelopes, boundary, product)
		if err != nil {
			return nil, err
		}
	}

	provisioned, err := s.Client.ListProvisionedProducts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Service Catalog provisioned products: %w", err)
	}
	for _, item := range provisioned {
		envelopes, err = appendProvisionedProduct(envelopes, boundary, item)
		if err != nil {
			return nil, err
		}
	}

	return envelopes, nil
}

func (s Scanner) appendPortfolio(
	ctx context.Context,
	envelopes []facts.Envelope,
	boundary awscloud.Boundary,
	portfolio Portfolio,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(portfolioObservation(boundary, portfolio))
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, resource)

	portfolioID := strings.TrimSpace(portfolio.ID)
	if portfolioID == "" {
		return envelopes, nil
	}
	principals, err := s.Client.PrincipalsForPortfolio(ctx, portfolioID)
	if err != nil {
		return nil, fmt.Errorf("list principals for portfolio %q: %w", portfolioID, err)
	}
	for _, relationship := range portfolioPrincipalRelationships(boundary, portfolio, principals) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func (s Scanner) appendProduct(
	ctx context.Context,
	envelopes []facts.Envelope,
	boundary awscloud.Boundary,
	product Product,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(productObservation(boundary, product))
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, resource)

	productID := strings.TrimSpace(product.ID)
	if productID == "" {
		return envelopes, nil
	}
	portfolios, err := s.Client.PortfoliosForProduct(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("list portfolios for product %q: %w", productID, err)
	}
	for _, relationship := range productInPortfolioRelationships(boundary, product, portfolios) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func appendProvisionedProduct(
	envelopes []facts.Envelope,
	boundary awscloud.Boundary,
	provisioned ProvisionedProduct,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(provisionedProductObservation(boundary, provisioned))
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, resource)

	if relationship := provisionedProductStackRelationship(boundary, provisioned); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func portfolioObservation(boundary awscloud.Boundary, portfolio Portfolio) awscloud.ResourceObservation {
	arn := strings.TrimSpace(portfolio.ARN)
	resourceID := firstNonEmpty(arn, portfolio.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeServiceCatalogPortfolio,
		Name:         strings.TrimSpace(portfolio.DisplayName),
		Attributes: map[string]any{
			"portfolio_id":  strings.TrimSpace(portfolio.ID),
			"display_name":  strings.TrimSpace(portfolio.DisplayName),
			"provider_name": strings.TrimSpace(portfolio.ProviderName),
			"description":   strings.TrimSpace(portfolio.Description),
			"created_time":  timeOrNil(portfolio.CreatedTime),
		},
		CorrelationAnchors: dedupeAnchors(arn, strings.TrimSpace(portfolio.ID)),
		SourceRecordID:     resourceID,
	}
}

func productObservation(boundary awscloud.Boundary, product Product) awscloud.ResourceObservation {
	arn := strings.TrimSpace(product.ARN)
	resourceID := firstNonEmpty(arn, product.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeServiceCatalogProduct,
		Name:         strings.TrimSpace(product.Name),
		State:        strings.TrimSpace(product.Status),
		Attributes: map[string]any{
			"product_id":   strings.TrimSpace(product.ID),
			"product_type": strings.TrimSpace(product.ProductType),
			"owner":        strings.TrimSpace(product.Owner),
			"distributor":  strings.TrimSpace(product.Distributor),
			"status":       strings.TrimSpace(product.Status),
			"created_time": timeOrNil(product.CreatedTime),
		},
		CorrelationAnchors: dedupeAnchors(arn, strings.TrimSpace(product.ID)),
		SourceRecordID:     resourceID,
	}
}

func provisionedProductObservation(
	boundary awscloud.Boundary,
	provisioned ProvisionedProduct,
) awscloud.ResourceObservation {
	arn := strings.TrimSpace(provisioned.ARN)
	resourceID := firstNonEmpty(arn, provisioned.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeServiceCatalogProvisionedProduct,
		Name:         strings.TrimSpace(provisioned.Name),
		State:        strings.TrimSpace(provisioned.Status),
		Attributes: map[string]any{
			"provisioned_product_id":     strings.TrimSpace(provisioned.ID),
			"status":                     strings.TrimSpace(provisioned.Status),
			"provisioned_product_type":   strings.TrimSpace(provisioned.Type),
			"product_id":                 strings.TrimSpace(provisioned.ProductID),
			"provisioning_artifact_id":   strings.TrimSpace(provisioned.ProvisioningArtifactID),
			"provisioning_artifact_name": strings.TrimSpace(provisioned.ProvisioningArtifactName),
			"physical_id":                strings.TrimSpace(provisioned.PhysicalID),
			"created_time":               timeOrNil(provisioned.CreatedTime),
		},
		CorrelationAnchors: dedupeAnchors(arn, strings.TrimSpace(provisioned.ID)),
		SourceRecordID:     resourceID,
	}
}

// dedupeAnchors returns the non-empty, de-duplicated correlation anchors in
// order. A blank ARN and a duplicate identifier are dropped so the anchor list
// stays stable across scans of identical Service Catalog state.
func dedupeAnchors(values ...string) []string {
	anchors := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		anchors = append(anchors, trimmed)
	}
	if len(anchors) == 0 {
		return nil
	}
	return anchors
}
