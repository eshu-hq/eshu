// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 App Mesh calls into scanner-owned
// metadata.
//
// The adapter is read-only. It calls only List and Describe operations plus
// ListTagsForResource across meshes, virtual services, virtual nodes, virtual
// routers, routes, virtual gateways, and gateway routes. It MUST NOT call any
// App Mesh mutation API (Create/Update/Delete for Mesh, VirtualService,
// VirtualNode, VirtualRouter, Route, VirtualGateway, GatewayRoute). The internal
// apiClient interface deliberately excludes every mutation method, and a
// reflection-based test verifies the exclusion so a future SDK refactor cannot
// quietly add one back.
//
// The adapter never returns a client TLS validation certificate body. Client
// TLS validation is reduced to the ACM Private CA certificate authority ARNs
// the trust references; file and SDS trust shapes (certificate chains, secret
// names) are not read. HTTP header match values are passed through to the
// scanner verbatim because the scanner, which holds the redaction key, owns the
// redaction decision.
package awssdk
