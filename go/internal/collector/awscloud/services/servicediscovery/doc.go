// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package servicediscovery maps AWS Cloud Map (Service Discovery) metadata into
// AWS cloud collector facts.
//
// The package owns scanner-level Cloud Map normalization only. It never calls
// the AWS SDK directly and never calls a Cloud Map mutation API. SDK adapters
// provide already-resolved Namespace values (each with its services attached),
// and Scanner emits aws_resource facts plus aws_relationship facts for the
// edges Cloud Map reports directly:
//
//   - service -> namespace (keyed by the Cloud Map namespace id)
//   - namespace -> Route 53 hosted zone (DNS namespaces only)
//
// The Cloud Map service resource is keyed by its "namespaceName/serviceName"
// identity, with resource type aws_cloud_map_service, so it resolves the App
// Mesh virtual-node-to-Cloud-Map-service edge, which targets the same
// namespace/service join key. The namespace -> hosted zone edge keys on the
// "/hostedzone/<id>" resource id the route53 scanner emits.
//
// Cloud Map does not report the VPC association for a private DNS namespace; the
// VPC is reached transitively through the private Route 53 hosted zone, which
// the route53 scanner owns. No VPC edge is invented here.
//
// One payload class is never persisted: instance attribute maps, which can hold
// caller-defined secrets. The scanner records the instance count from the Cloud
// Map service summary and never reads, lists, or discovers instances.
package servicediscovery
