// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the CodePipeline scanner with the awsruntime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the CodePipeline scanner builder to the registry so
// DefaultScannerFactory can resolve service_kind "codepipeline" without a
// central switch. The builder returns a typed error when the redaction key is
// zero because CodePipeline redacts source-revision summaries. Production
// callers pull every service binding through
// internal/collector/awscloud/awsruntime/bindings.
package runtimebind
