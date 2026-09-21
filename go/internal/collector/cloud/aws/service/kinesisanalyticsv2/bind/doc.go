// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the Managed Service for Apache Flink (Kinesis
// Data Analytics v2) scanner with the awsruntime registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the Managed Flink scanner builder to the registry so
// DefaultScannerFactory can resolve service_kind "kinesisanalyticsv2" without a
// central switch. Production callers pull every service binding through
// internal/collector/awscloud/awsruntime/bindings.
package runtimebind
