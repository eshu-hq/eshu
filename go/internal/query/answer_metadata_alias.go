// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// AnswerMetadata is the normalized, additive answer companion attached to
// story and investigation responses. The implementation moved to
// internal/query/querycontract (#6060) so handler-family subpackages can
// attach it without importing this package; this alias keeps root's own
// handlers, packet builders, and tests compiling unchanged.
type AnswerMetadata = querycontract.AnswerMetadata

// attachAnswerMetadata derives the normalized answer companion from an
// already-built response payload and stores it under "answer_metadata". The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func attachAnswerMetadata(data map[string]any) map[string]any {
	return querycontract.AttachAnswerMetadata(data)
}

// BuildAnswerMetadata derives normalized answer metadata from an
// already-built response payload. The implementation moved to querycontract
// for #6060; this wrapper keeps root callers unchanged.
func BuildAnswerMetadata(data map[string]any) AnswerMetadata {
	return querycontract.BuildAnswerMetadata(data)
}

// AnswerMetadataFromData extracts normalized answer metadata from a response
// map. The implementation moved to querycontract for #6060; this wrapper
// keeps root callers unchanged.
func AnswerMetadataFromData(data map[string]any) (AnswerMetadata, bool) {
	return querycontract.AnswerMetadataFromData(data)
}
