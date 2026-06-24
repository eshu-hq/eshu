// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssctypes "github.com/aws/aws-sdk-go-v2/service/servicecatalog/types"

	scservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/servicecatalog"
)

func mapPortfolio(detail awssctypes.PortfolioDetail) scservice.Portfolio {
	return scservice.Portfolio{
		ID:           strings.TrimSpace(aws.ToString(detail.Id)),
		ARN:          strings.TrimSpace(aws.ToString(detail.ARN)),
		DisplayName:  strings.TrimSpace(aws.ToString(detail.DisplayName)),
		ProviderName: strings.TrimSpace(aws.ToString(detail.ProviderName)),
		Description:  strings.TrimSpace(aws.ToString(detail.Description)),
		CreatedTime:  aws.ToTime(detail.CreatedTime),
	}
}

func mapProduct(detail awssctypes.ProductViewDetail) scservice.Product {
	product := scservice.Product{
		ARN:         strings.TrimSpace(aws.ToString(detail.ProductARN)),
		Status:      strings.TrimSpace(string(detail.Status)),
		CreatedTime: aws.ToTime(detail.CreatedTime),
	}
	if summary := detail.ProductViewSummary; summary != nil {
		product.ID = strings.TrimSpace(aws.ToString(summary.ProductId))
		product.Name = strings.TrimSpace(aws.ToString(summary.Name))
		product.ProductType = strings.TrimSpace(string(summary.Type))
		product.Owner = strings.TrimSpace(aws.ToString(summary.Owner))
		product.Distributor = strings.TrimSpace(aws.ToString(summary.Distributor))
	}
	return product
}

func mapProvisionedProduct(detail awssctypes.ProvisionedProductDetail) scservice.ProvisionedProduct {
	return scservice.ProvisionedProduct{
		ID:                     strings.TrimSpace(aws.ToString(detail.Id)),
		ARN:                    strings.TrimSpace(aws.ToString(detail.Arn)),
		Name:                   strings.TrimSpace(aws.ToString(detail.Name)),
		Status:                 strings.TrimSpace(string(detail.Status)),
		Type:                   strings.TrimSpace(aws.ToString(detail.Type)),
		ProductID:              strings.TrimSpace(aws.ToString(detail.ProductId)),
		ProvisioningArtifactID: strings.TrimSpace(aws.ToString(detail.ProvisioningArtifactId)),
		CreatedTime:            aws.ToTime(detail.CreatedTime),
	}
}

func mapPrincipal(principal awssctypes.Principal) scservice.Principal {
	return scservice.Principal{
		ARN:  strings.TrimSpace(aws.ToString(principal.PrincipalARN)),
		Type: strings.TrimSpace(string(principal.PrincipalType)),
	}
}
