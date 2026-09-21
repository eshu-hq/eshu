// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the X-Ray scanner with the awsruntime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the X-Ray scanner builder to the registry so DefaultScannerFactory can
// resolve service_kind "xray" without a central switch. The X-Ray
// configuration scanner carries no secret-shaped fields, so the binding sets no
// RequiresRedactionKey flag. Production callers pull every service binding
// through internal/collector/awscloud/awsruntime/bindings.
package runtimebind
