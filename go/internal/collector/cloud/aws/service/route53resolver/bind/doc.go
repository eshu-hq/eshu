// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package bind registers the Route 53 Resolver scanner with the
// runtime registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the Route 53 Resolver scanner builder to the registry so
// DefaultScannerFactory can resolve service_kind "route53resolver" without a
// central switch. The binding declares no redaction-key requirement: the
// scanner drops DNS Firewall domain list contents by never mapping them.
// Production callers pull every service binding through
// internal/collector/cloud/aws/runtime/bindings.
package bind
