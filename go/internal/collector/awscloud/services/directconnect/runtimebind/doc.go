// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the Direct Connect scanner with the awsruntime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the Direct Connect scanner builder to the registry so
// DefaultScannerFactory can resolve service_kind "directconnect" without a
// central switch. The builder needs no redaction key: Direct Connect drops the
// BGP authentication key and MACsec key material by never mapping them, so the
// registration leaves RequiresRedactionKey unset. Production callers pull every
// service binding through internal/collector/awscloud/awsruntime/bindings.
package runtimebind
