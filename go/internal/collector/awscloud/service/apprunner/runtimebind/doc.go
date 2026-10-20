// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the App Runner scanner with the awsruntime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the App Runner scanner builder to the registry so DefaultScannerFactory
// can resolve service_kind "apprunner" without a central switch. App Runner
// needs no redaction key because runtime environment-variable values are
// dropped rather than redacted, so the registration leaves RequiresRedactionKey
// unset. Production callers pull every service binding through
// internal/collector/awscloud/awsruntime/bindings.
package runtimebind
