// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// failureSite is a closed set of public descriptions. Driver errors stay in
// Unwrap for classification, but their connection metadata is never formatted.
type failureSite uint8

const (
	failureWriterIdentity failureSite = iota
	failureWriterPing
	failureReaderPing
	failureWriterCheckpoint
	failureReaderBorrow
	failureReaderIdentity
	failureReaderReplay
	failureReaderQuery
	failureReaderRows
	failureSnapshotBegin
	failureSnapshotTerminal
	failureRawBytes
	failurePoolClose
)

type privateError struct {
	site  failureSite
	cause error
}

// Error exposes only the fixed failure stage, excluding driver metadata.
func (e privateError) Error() string {
	switch e.site {
	case failureWriterIdentity:
		return "PostgreSQL writer identity unavailable"
	case failureWriterPing:
		return "PostgreSQL writer unavailable"
	case failureReaderPing:
		return "PostgreSQL reader unavailable"
	case failureWriterCheckpoint:
		return "PostgreSQL writer checkpoint failed"
	case failureReaderBorrow:
		return "PostgreSQL reader connection unavailable"
	case failureReaderIdentity:
		return "PostgreSQL reader identity check failed"
	case failureReaderReplay:
		return "PostgreSQL reader replay check failed"
	case failureReaderQuery:
		return "PostgreSQL reader query failed"
	case failureReaderRows:
		return "PostgreSQL reader cursor failed"
	case failureSnapshotBegin:
		return "PostgreSQL reader snapshot failed to begin"
	case failureSnapshotTerminal:
		return "PostgreSQL reader snapshot failed to finish"
	case failureRawBytes:
		return "sql: RawBytes is unsupported on guarded reader rows; use *[]byte"
	case failurePoolClose:
		return "PostgreSQL access failed to close"
	default:
		return "PostgreSQL reader failed"
	}
}

// Unwrap preserves the driver cause for internal errors.Is and errors.As checks.
func (e privateError) Unwrap() error { return e.cause }

// GoString keeps Go-syntax formatting from exposing the private cause fields.
func (e privateError) GoString() string { return e.Error() }

func privateFailure(site failureSite, cause error) error {
	if cause == nil {
		return nil
	}
	return privateError{site: site, cause: cause}
}
