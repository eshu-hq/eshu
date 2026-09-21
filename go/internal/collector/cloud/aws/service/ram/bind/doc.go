// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the AWS Resource Access Manager scanner with
// the awsruntime registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the RAM scanner builder to the registry so DefaultScannerFactory can
// resolve service_kind "ram" without a central switch. The builder needs no
// redaction key because the RAM scanner is metadata-only and redacts nothing.
// Production callers pull every service binding through
// internal/collector/awscloud/awsruntime/bindings.
package runtimebind
