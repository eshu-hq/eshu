// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cassette implements credential-free collector replay from a
// pre-recorded JSON cassette file. A cassette captures the collector output
// (scope identity, generation metadata, and fact envelopes) of one or more
// credentialed collection runs so the same facts can be replayed without live
// credentials. Collector binaries expose a -mode=cassette -cassette-file=<path>
// flag pair that wires this package as the source.
package cassette
