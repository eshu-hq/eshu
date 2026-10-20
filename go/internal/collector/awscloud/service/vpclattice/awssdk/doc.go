// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 VPC Lattice client into the
// metadata-only VPC Lattice scanner interface.
//
// The adapter uses ListServiceNetworks, ListServiceNetworkVpcAssociations,
// ListServiceNetworkServiceAssociations, ListServices, GetService,
// ListListeners, ListTargetGroups, GetTargetGroup, ListTargets, and
// ListTagsForResource to read VPC Lattice control-plane metadata, association
// evidence, and resource tags. It intentionally excludes GetAuthPolicy,
// GetResourcePolicy, and every Create/Update/Delete/Put/Register/Deregister
// mutation API, so the adapter cannot read policy bodies or mutate VPC Lattice
// state. GetService and GetTargetGroup are read-only detail reads used for the
// certificate ARN and backing VPC identifier the list summaries omit.
package awssdk
