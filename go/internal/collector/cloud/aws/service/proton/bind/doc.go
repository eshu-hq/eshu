// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind registers the AWS Proton scanner with the awsruntime
// registry through an init side effect.
//
// Importing this package for its side effect installs a builder for the
// proton service_kind that constructs the Proton scanner with the SDK adapter
// from claim-scoped ScannerDeps. It does no work at load time beyond the single
// Register call.
package runtimebind
