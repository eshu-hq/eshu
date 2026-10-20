// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 OpenSearch and OpenSearch
// Serverless clients into the metadata-only OpenSearch scanner interface.
//
// The adapter uses ListDomainNames, DescribeDomains, DescribePackages,
// ListDomainsForPackage, ListTags, ListCollections, BatchGetCollection,
// ListSecurityConfigs, ListVpcEndpoints, and BatchGetVpcEndpoint. It never
// reaches the OpenSearch HTTP API (_search, _msearch, _index, _doc, _bulk, and
// similar): that API is not part of the AWS SDK client, so it is unreachable
// from this code path by construction. The adapter excludes every mutation
// (CreateDomain, DeleteDomain, UpdateDomainConfig, CreateCollection,
// DeleteCollection, CreatePackage, DeletePackage, AssociatePackage,
// DissociatePackage), the inbound-connection acceptance API
// (AcceptInboundConnection), and the GetIndex reads on both services.
//
// The DescribeDomains response does not return the master user password, and
// the adapter never persists the domain endpoint, endpoints map, access policy
// body, custom package body, or serverless saved-object body. Only IAM role
// ARNs referenced by the domain access policy are resolved, for relationship
// evidence; the policy body itself is dropped.
package awssdk
