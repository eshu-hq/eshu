// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "github.com/eshu-hq/eshu/go/internal/query/querycontract/answer"

// AnswerMetadata is the normalized, additive answer companion attached to
// story and investigation responses. The implementation moved to
// querycontract for #6060 so handler-family subpackages can attach it without
// importing this package, and on to querycontract/answer for #6597; this alias
// keeps root's own handlers and internal/mcp, which names
// query.AnswerMetadata, compiling unchanged.
type AnswerMetadata = answer.AnswerMetadata
