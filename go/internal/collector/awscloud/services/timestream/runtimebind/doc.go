// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the Timestream scanner with the awsruntime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the Timestream scanner builder to the registry so DefaultScannerFactory
// can resolve service_kind "timestream" without a central switch. Production
// callers pull every service binding through
// internal/collector/awscloud/awsruntime/bindings.
package runtimebind
