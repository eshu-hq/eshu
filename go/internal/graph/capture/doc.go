// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package capture persists and diffs the differential statement
// recordings that issue #6782's capture wrappers
// ([backendconformance.WrapGraphQuery] and
// [backendconformance.WrapExecutor]) produce during a golden-corpus replay.
//
// The capture wrappers record into memory; the services under test run as
// separate binaries (bootstrap-index, projector, reducer, api, mcp-server),
// so a recording must survive its process to be compared. A [Session]
// holds one process's recorder and appends it as JSONL to the directory
// named by ESHU_DIFFERENTIAL_CAPTURE_DIR when the session closes. [LoadDir]
// reads both backends' files back, and [Compare] diffs them with
// [backendconformance.CompareRecordings], excusing divergences the
// [Allowlist] names.
//
// Capture stays out of the hot path: [Open] returns a nil session unless
// [backendconformance.CaptureEnabled] opts in and a directory is set, and
// every decorator is a passthrough on a nil session. Recordings contain
// statement parameters, so capture refuses to run against non-gate query
// profiles (see [Open]).
package capture
