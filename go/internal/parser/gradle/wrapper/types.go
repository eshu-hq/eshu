// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wrapper

const (
	// LanguageName is the lang tag on every payload and row.
	LanguageName = "gradle"
	// KeyDistributionURL is the only property the parser reads.
	KeyDistributionURL = "distributionUrl"
	// StateKey is the payload key that carries the scan state and counts.
	StateKey = "gradle_wrapper_state"

	// MaxSourceBytes bounds the file the parser will scan. A generated wrapper
	// file is under 400 bytes.
	MaxSourceBytes = 64 << 10
	// MaxValueBytes bounds the unescaped distributionUrl value.
	MaxValueBytes = 2048
	// MaxVersionBytes bounds the version text taken from the archive name.
	MaxVersionBytes = 64
)

// File states reported in Result.State.
const (
	// StateParsed means the file was scanned.
	StateParsed = "parsed"
	// StateOversize means the file exceeds MaxSourceBytes and was not scanned.
	StateOversize = "oversize"
)

// Counts tallies what a scan could not turn into a row.
type Counts struct {
	// Unsupported counts distributionUrl declarations the grammar does not
	// cover, and keys written with an escape. None yields a row.
	Unsupported int
}

// Result is the outcome of scanning one wrapper properties file.
type Result struct {
	// State is StateParsed or StateOversize.
	State string
	// ParseError names why State is not StateParsed.
	ParseError string
	// Rows holds the distribution row, when the grammar matched.
	Rows []map[string]any
	// Counts tallies the scan.
	Counts Counts
}
