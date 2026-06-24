// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the CloudFormation scanner with the awsruntime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the CloudFormation scanner builder to the registry so
// DefaultScannerFactory can resolve service_kind "cloudformation" without a
// central switch. The builder requires a non-zero ScannerDeps.RedactionKey
// because the scanner redacts secret-like stack output values, and returns a
// typed error when the key is zero. Production callers pull every service
// binding through internal/collector/awscloud/awsruntime/bindings.
package runtimebind
