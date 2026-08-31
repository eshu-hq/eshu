// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package packagereg implements the package-registry query handler family:
// graph-backed package/version identity reads, package-native dependency
// edges, reducer-derived correlation reads, and graph aggregate/inventory
// reads, all served under PackageRegistryHandler.
//
// It moved out of root package query (#6060) so this handler family can be
// read, tested, and changed without pulling in the rest of the query
// surface. It depends only on the dependency-neutral leaf packages under
// internal/query -- querycontract (ports, row-value decoders, response and
// truth envelopes, capability gates), querydecode (classified fact-decode
// failures), queryselector (repository-selector resolution), and queryspan
// (the per-route HTTP span) -- never on root package query itself, which
// would create an import cycle: root's package_registry_alias.go imports
// packagereg for the PackageRegistryHandler/PackageRegistryCorrelationRow
// compatibility aliases cmd/api and cmd/mcp-server still use.
//
// This package registers its own capabilities with querycontract
// (package_registry_capabilities.go) rather than through root's legacy
// capabilityMatrix compatibility map, for the same cycle reason -- and
// because go test ./internal/query/packagereg never runs root's init()
// functions, a registration left in root would leave every capability gate
// in this package's own tests reporting unsupported_capability.
package packagereg
