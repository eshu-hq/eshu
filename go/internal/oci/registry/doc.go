// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package registry owns the shared OCI repository identity the
// coordinator planner resolves from configured targets: the provider
// vocabulary, the raw and normalized repository identity, the
// provider-neutral normalization rules, and the pure per-provider
// repository-name and endpoint parsing the planner needs to build an
// identity without opening a registry connection.
//
// The provider distribution clients stay in the extracted satellites, which
// carry their own copies: nothing here dials a registry, fetches metadata,
// or holds credentials.
package registry
