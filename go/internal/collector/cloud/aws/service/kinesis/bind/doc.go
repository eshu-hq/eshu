// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package bind registers the Kinesis scanner with the runtime
// registry.
//
// The package has no exported surface. Importing it for its init side effect
// adds the Kinesis scanner builder to the registry so DefaultScannerFactory
// can resolve service_kind "kinesis" without a central switch. The builder
// covers Kinesis Data Streams, Kinesis Data Firehose, and Kinesis Video
// Streams. Production callers pull every service binding through
// internal/collector/cloud/aws/runtime/bindings.
package bind
