// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind self-registers the Route 53 Application Recovery Controller
// recovery-control configuration scanner with the AWS runtime scanner registry.
//
// Importing this package for its init side effect installs a builder under the
// route53recoverycontrolconfig service kind that wires the SDK adapter into the
// scanner. The production binding aggregator imports it blank so the scanner is
// reachable through awsruntime.DefaultScannerFactory.
package runtimebind
