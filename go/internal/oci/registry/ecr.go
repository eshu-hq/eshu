// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import (
	"fmt"
	"strings"
)

// ECRPrivateRegistryHost returns the AWS account and region scoped ECR
// registry host.
func ECRPrivateRegistryHost(accountID, region string) (string, error) {
	accountID = strings.TrimSpace(accountID)
	region = strings.TrimSpace(region)
	if accountID == "" {
		return "", fmt.Errorf("ecr account id is required")
	}
	if region == "" {
		return "", fmt.Errorf("ecr region is required")
	}
	return accountID + ".dkr.ecr." + region + ".amazonaws.com", nil
}

// ECRRepositoryIdentity builds the shared OCI identity for an ECR repository.
func ECRRepositoryIdentity(registryHost, repository string) RepositoryIdentity {
	return RepositoryIdentity{
		Provider:   ProviderECR,
		Registry:   registryHost,
		Repository: repository,
	}
}
