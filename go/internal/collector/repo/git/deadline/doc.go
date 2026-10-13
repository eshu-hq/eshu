// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package deadline bounds each git command the git collector's repository sync
// runs (#8052).
//
// The sync loop is sequential, so one git command stuck on name resolution
// otherwise pauses the whole fleet sync until libcurl gives up after about
// 300 s. Run derives a deadline from the caller's context, runs the command in
// its process group, and on expiry kills git and its remote-https helper
// together and returns an Error that names the deadline and the elapsed time.
// The initial clone has its own longer limit, because a large clone that is
// still progressing is not a stall. A cancel of the parent context is never
// reported or counted as a deadline.
package deadline
