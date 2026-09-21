// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the API Gateway v2 scanner with the awsruntime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the API Gateway v2 scanner builder to the registry so
// DefaultScannerFactory can resolve service_kind "apigatewayv2" without a
// central switch. The scanner needs no redaction key because it drops templates
// and secrets by never mapping them, so the registration leaves
// RequiresRedactionKey unset. Production callers pull every service binding
// through internal/collector/awscloud/awsruntime/bindings.
package runtimebind
