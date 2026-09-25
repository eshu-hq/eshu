// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package bind registers the App Mesh scanner with the runtime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the App Mesh scanner builder to the registry so DefaultScannerFactory
// can resolve service_kind "appmesh" without a central switch. The builder
// returns a typed error when the redaction key is zero because App Mesh redacts
// sensitive HTTP header match values. Production callers pull every service
// binding through internal/collector/cloud/aws/runtime/bindings.
package bind
