// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package deadline bounds each network-capable git command the git collector's
// repository sync runs (#8052).
//
// The sync loop is sequential, so one git command stuck on name resolution
// otherwise pauses the whole fleet sync until libcurl gives up after about
// 300 s. Run derives a deadline from the caller's context and runs the command
// the caller built from it. The caller builds it with
// runtime.NewProcessGroupCommand, so on expiry the kill reaches git and its
// remote-https helper together; Run then returns an Error that names the
// deadline and the elapsed time.
// Ref listing, fetch, and clone have their own limits (60, 90, and 600 s by
// default); the initial clone has the longest, because a large clone that is
// still progressing is not a stall. A cancel of the parent context is never
// reported or counted as a deadline.
package deadline
