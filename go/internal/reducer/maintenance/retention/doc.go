// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package retention prunes superseded source-generation history in
// bounded Postgres transactions.
//
// [Runner] drains eligible batches beside normal reducer intent
// processing: a pass that prunes loops immediately, a pass that finds
// nothing sleeps the poll interval, and a pass that finds candidates
// but prunes none retries soon with backoff bounded by the poll
// interval (#7398). Lock-held candidates stay invisible by design (the
// candidate SELECT uses SKIP LOCKED), so a lock-starved pass keeps the
// full sleep. A cycle the pruner refuses with [ErrKeyIndexUnavailable]
// reports failure reason key_index_unavailable and retries on its next
// poll; an operator rebuilds the index to clear it.
package retention
