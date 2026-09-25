// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package opensearch maps Amazon OpenSearch metadata into AWS cloud collector
// facts.
//
// The scanner emits reported-confidence resources for OpenSearch Service
// provisioned domains, OpenSearch custom packages, and OpenSearch Serverless
// collections, security configurations, and managed VPC endpoints. It emits
// relationships for domain VPC, subnet, security group, KMS-key, and
// master-user IAM-role references, package-to-domain associations, and
// collection-to-KMS-key references. It does not emit a collection-to-VPC-endpoint
// edge: Serverless reports no collection-to-endpoint binding in either record,
// so a per-endpoint edge would be a misleading cross-product.
//
// The package never reaches the OpenSearch HTTP API (_search, _msearch,
// _index, _doc, _bulk, and similar) and never persists master user passwords
// (the AWS DescribeDomains response does not return the password), domain
// endpoint contents, access policy bodies, custom package bodies, or
// serverless saved-object bodies. Mutation APIs (CreateDomain, DeleteDomain,
// UpdateDomainConfig, CreateCollection, DeleteCollection, CreatePackage,
// DeletePackage, AssociatePackage, DissociatePackage,
// AcceptInboundConnection, and related calls) stay outside this package
// contract.
package opensearch
