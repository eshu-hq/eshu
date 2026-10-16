// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package targeted owns the partition-scoped deferred relationship maintenance
// pass. It discovers the owed partition closure, writes evidence in bounded
// repository batches, publishes readiness, and reopens succeeded reducer work
// in one transaction. The parent supplies phase, queue, and memo collaborators.
package targeted
