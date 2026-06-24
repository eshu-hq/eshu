// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the AWS Batch scanner with the awsruntime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the Batch scanner builder to the registry so DefaultScannerFactory can
// resolve service_kind "batch" without a central switch. The builder returns a
// typed error when the redaction key is absent, because Batch job-definition
// container environment values route through the shared redact library.
// Production callers pull every service binding through
// internal/collector/awscloud/awsruntime/bindings.
package runtimebind
