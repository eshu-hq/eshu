// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ram maps AWS Resource Access Manager observations into AWS cloud
// fact envelopes.
//
// The package owns scanner-level RAM fact selection for resource shares the
// account owns, the resources those shares share, the principals they target,
// and the managed permissions they use, plus the relationships between them. It
// is metadata-only and never persists a permission policy document body: the
// scanner-owned Permission type carries only name, ARN, version, type, and
// status, so a policy-body leak does not compile. AWS SDK pagination,
// credentials, persistence, graph projection, and reducer-owned correlation
// live outside this package.
package ram
