// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package bind registers the DRS scanner with the runtime registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the AWS Elastic Disaster Recovery scanner builder to the registry so
// DefaultScannerFactory can resolve service_kind "drs" without a central switch.
// Production callers pull every service binding through
// internal/collector/cloud/aws/runtime/bindings.
package bind
