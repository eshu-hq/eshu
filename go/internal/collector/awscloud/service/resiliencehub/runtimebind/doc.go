// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the AWS Resilience Hub scanner with the AWS
// runtime scanner registry through an init side effect.
//
// Importing this package for its side effect (typically from the aggregate
// bindings package) installs the resiliencehub scanner builder so
// awsruntime.DefaultScannerFactory can construct it for a claimed boundary.
package runtimebind
