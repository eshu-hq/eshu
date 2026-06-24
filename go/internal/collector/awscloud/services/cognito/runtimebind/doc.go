// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the Cognito scanner with the awsruntime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the Cognito scanner builder to the registry so DefaultScannerFactory can
// resolve service_kind "cognito" without a central switch. The builder returns
// a typed error when the runtime redaction key is zero, mirroring the ECS and
// Lambda bindings. Production callers pull every service binding through
// internal/collector/awscloud/awsruntime/bindings.
package runtimebind
