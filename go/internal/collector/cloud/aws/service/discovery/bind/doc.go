// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the Cloud Map (Service Discovery) scanner with
// the awsruntime registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the Cloud Map scanner builder to the registry so DefaultScannerFactory
// can resolve service_kind "servicediscovery" without a central switch. The
// scanner needs no redaction key because it records instance counts only and
// never reads instance attribute maps, so the registration leaves
// RequiresRedactionKey unset. Production callers pull every service binding
// through internal/collector/awscloud/awsruntime/bindings.
package runtimebind
