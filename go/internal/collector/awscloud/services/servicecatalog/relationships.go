// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// provisionedProductCFNStackType is the AWS-reported provisioned-product type
// whose physical identifier is a CloudFormation stack ARN. Service Catalog also
// supports TERRAFORM_OPEN_SOURCE and TERRAFORM_CLOUD provisioned products,
// whose physical identifiers are not CloudFormation stacks, so the stack edge
// is gated on this exact type.
const provisionedProductCFNStackType = "CFN_STACK"

// provisionedProductStackRelationship records the CloudFormation stack a
// provisioned product deploys. It is emitted only for CFN_STACK provisioned
// products whose physical identifier is a CloudFormation stack ARN. The edge is
// ARN-keyed: the CloudFormation scanner publishes a stack node's resource_id as
// the stack ARN, so the target id and target ARN are both the stack ARN. The
// source id is firstNonEmpty(arn, id), matching the provisioned-product node's
// own resource_id, so the outgoing edge resolves to its source node.
func provisionedProductStackRelationship(
	boundary awscloud.Boundary,
	provisioned ProvisionedProduct,
) *awscloud.RelationshipObservation {
	if !strings.EqualFold(strings.TrimSpace(provisioned.Type), provisionedProductCFNStackType) {
		return nil
	}
	stackARN := strings.TrimSpace(provisioned.PhysicalID)
	if !isCloudFormationStackARN(stackARN) {
		return nil
	}
	sourceID := firstNonEmpty(provisioned.ARN, provisioned.ID)
	if sourceID == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipServiceCatalogProvisionedProductDeploysCloudFormationStack,
		SourceResourceID: sourceID,
		SourceARN:        strings.TrimSpace(provisioned.ARN),
		TargetResourceID: stackARN,
		TargetARN:        stackARN,
		TargetType:       awscloud.ResourceTypeCloudFormationStack,
		SourceRecordID: sourceID + "->" +
			awscloud.RelationshipServiceCatalogProvisionedProductDeploysCloudFormationStack +
			":" + stackARN,
	}
}

// productInPortfolioRelationships records the portfolios a product is
// associated with. Each edge's source id is firstNonEmpty(product arn, id),
// matching the product node's own resource_id, and the target id is
// firstNonEmpty(portfolio arn, id), matching how the portfolio node publishes
// its resource_id. Portfolio ARNs are preferred so the edge is ARN-keyed and
// joins the portfolio node by ARN equality.
func productInPortfolioRelationships(
	boundary awscloud.Boundary,
	product Product,
	portfolios []Portfolio,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(product.ARN, product.ID)
	if sourceID == "" || len(portfolios) == 0 {
		return nil
	}
	observations := make([]awscloud.RelationshipObservation, 0, len(portfolios))
	seen := make(map[string]struct{}, len(portfolios))
	for _, portfolio := range portfolios {
		targetID := firstNonEmpty(portfolio.ARN, portfolio.ID)
		if targetID == "" {
			continue
		}
		if _, ok := seen[targetID]; ok {
			continue
		}
		seen[targetID] = struct{}{}
		observation := awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipServiceCatalogProductInPortfolio,
			SourceResourceID: sourceID,
			SourceARN:        strings.TrimSpace(product.ARN),
			TargetResourceID: targetID,
			TargetType:       awscloud.ResourceTypeServiceCatalogPortfolio,
			SourceRecordID: sourceID + "->" +
				awscloud.RelationshipServiceCatalogProductInPortfolio + ":" + targetID,
		}
		if portfolioARN := strings.TrimSpace(portfolio.ARN); portfolioARN != "" {
			observation.TargetARN = portfolioARN
		}
		observations = append(observations, observation)
	}
	if len(observations) == 0 {
		return nil
	}
	return observations
}

// portfolioPrincipalRelationships records the IAM role principals associated
// with a portfolio. An edge is emitted only when AWS reports a fully defined
// IAM role ARN; IAM_PATTERN wildcard principals and non-role principals (users,
// groups) name no concrete IAM role node and are skipped. The edge is
// ARN-keyed: the IAM scanner publishes a role node's resource_id as the role
// ARN. The source id is firstNonEmpty(portfolio arn, id), matching the
// portfolio node's own resource_id.
func portfolioPrincipalRelationships(
	boundary awscloud.Boundary,
	portfolio Portfolio,
	principals []Principal,
) []awscloud.RelationshipObservation {
	sourceID := firstNonEmpty(portfolio.ARN, portfolio.ID)
	if sourceID == "" || len(principals) == 0 {
		return nil
	}
	observations := make([]awscloud.RelationshipObservation, 0, len(principals))
	seen := make(map[string]struct{}, len(principals))
	for _, principal := range principals {
		roleARN := strings.TrimSpace(principal.ARN)
		if !isIAMRoleARN(roleARN) {
			continue
		}
		if _, ok := seen[roleARN]; ok {
			continue
		}
		seen[roleARN] = struct{}{}
		observation := awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipServiceCatalogPortfolioGrantsPrincipal,
			SourceResourceID: sourceID,
			SourceARN:        strings.TrimSpace(portfolio.ARN),
			TargetResourceID: roleARN,
			TargetARN:        roleARN,
			TargetType:       awscloud.ResourceTypeIAMRole,
			SourceRecordID: sourceID + "->" +
				awscloud.RelationshipServiceCatalogPortfolioGrantsPrincipal + ":" + roleARN,
		}
		if principalType := strings.TrimSpace(principal.Type); principalType != "" {
			observation.Attributes = map[string]any{"principal_type": principalType}
		}
		observations = append(observations, observation)
	}
	if len(observations) == 0 {
		return nil
	}
	return observations
}
