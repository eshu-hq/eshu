// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 EC2 Image Builder client into the
// metadata-only Image Builder scanner interface.
//
// The adapter uses ListImagePipelines, ListImageRecipes/GetImageRecipe,
// ListContainerRecipes/GetContainerRecipe,
// ListInfrastructureConfigurations/GetInfrastructureConfiguration, and
// ListDistributionConfigurations/GetDistributionConfiguration to read Image
// Builder control-plane metadata. It intentionally excludes every Create/Update/
// Delete mutation, every Start/Import/Cancel run-control API, and the component
// build-version, image build-version, image-scan-finding, and workflow reads, so
// the adapter cannot read or persist component build-document bodies, Dockerfile
// bodies, instance user data, scan findings, or build artifacts.
package awssdk
