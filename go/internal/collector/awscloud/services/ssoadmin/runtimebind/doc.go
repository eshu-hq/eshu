// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the IAM Identity Center (ssoadmin) scanner with
// the awsruntime registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the ssoadmin scanner builder to the registry so DefaultScannerFactory
// can resolve service_kind "ssoadmin" without a central switch. The builder
// returns a typed error when the redaction key is zero because principal
// display names are redacted before persistence. Production callers pull every
// service binding through internal/collector/awscloud/awsruntime/bindings.
package runtimebind
