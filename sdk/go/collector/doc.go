// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package collector defines the public Eshu collector extension SDK contracts.
//
// The SDK is intentionally independent from Eshu internal Go packages. It
// describes the collector-sdk/v1alpha1 wire records an out-of-tree collector can
// return to a core-owned extension host, plus validators that fail closed before
// the host commits facts.
package collector
