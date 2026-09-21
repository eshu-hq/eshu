// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the EC2 Auto Scaling scanner with the
// awsruntime registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the Auto Scaling scanner builder to the registry so DefaultScannerFactory
// can resolve service_kind "autoscaling" without a central switch. The scanner
// declares no redaction-key requirement because it drops launch configuration
// and launch template UserData by never mapping it. Production callers pull
// every service binding through internal/collector/awscloud/awsruntime/bindings.
package runtimebind
