// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package contract defines the dependency-neutral reducer registry vocabulary.
//
// Reducer family packages import these domain constants, intent, result,
// ownership, and handler contracts without importing the parent reducer
// package. The parent package aliases this surface for compatibility and
// retains registry assembly, runtime execution, queue behavior, and backend
// wiring. ParseDomain accepts reducer intent domains only; shared-projection
// constants remain names for their dedicated runners.
package contract
