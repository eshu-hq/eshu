// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/decode"
)

// This file holds the decode-error substrate shared across root's remaining
// factschema_decode_*.go files (today only factschema_decode_supplychain.go).
// It was factschema_decode_workitem.go until the work-item decode wrappers
// themselves moved to internal/query/workitem/factschema_decode.go (#6642);
// what stays here is only what factschema_decode_supplychain.go still needs:
// the classified decode-error alias/wrapper, the default schema-version
// literal, and derefString ("the prefix lied" -- it was never
// work-item-specific, it derefs any typed pointer field). The former
// derefBool twin had no caller left after the move and was dropped.
//
// This mirrors the projector's factschema_quarantine.go pattern
// (partitionProjectorDecodeFailures / newProjectorDecodeError /
// factschemaEnvelope) at the scope this package actually needs: the query
// layer never quarantines a durable fact record (it is a read path, not a
// write path), it classifies one decoded row as unusable for the response it
// is building.

// queryDecodeError is the query layer's classified decode failure. It aliases
// decode.Error, which owns the type and both of its methods.
//
// The alias carries Error() and Unwrap() because they are exported; #6060's
// other seam, RepositoryAccessFilter, needed a 177-file rename precisely
// because ITS methods were unexported and an alias cannot reach those across a
// package boundary. Nothing here changes for the existing references.
type queryDecodeError = decode.Error

// newQueryDecodeError wraps a decode error returned by a factschema Decode*
// function into the query layer's classified decode failure.
func newQueryDecodeError(factKind, factID string, err error) *queryDecodeError {
	return decode.New(factKind, factID, err)
}

// queryDefaultSchemaMajorVersion is the schema version this package assumes
// when a row carries none. It is a major-1 version because every migrated
// supply-chain source-fact kind that uses it is at schema major 1 today; the
// Decode seam dispatches on the major component only.
const queryDefaultSchemaMajorVersion = "1.0.0"

// derefString returns the value a *string points at, or "" when it is nil,
// matching the pre-typing StringVal("") behavior for a field this migration
// converts from a raw payload lookup to a typed pointer.
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
