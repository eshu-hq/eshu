// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package verifiedaccess maps Amazon Verified Access instance, group, endpoint,
// and trust-provider metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for Verified Access instances,
// groups, endpoints, and trust providers plus relationships for group-in-instance
// and endpoint-in-group membership, instance-to-trust-provider attachment, and
// endpoint dependencies on EC2 subnets, EC2 security groups, and ACM
// certificates. Although Verified Access ships under the EC2 SDK, it is its own
// service kind ("verifiedaccess") with its own ResourceType constants, distinct
// from the core EC2 scanner. Trust-provider client secrets, OIDC client
// identifiers, group/endpoint policy documents, and any data-plane payload stay
// outside this package contract: the scanner is metadata-only.
package verifiedaccess
