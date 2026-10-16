// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"errors"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Error codes a rank walk answers with. They extend the query error
// vocabulary (querycontract.ErrorCode) for the two routes that accept
// rank_cursor.
const (
	// ErrorCodeInvalid is HTTP 400: the rank_cursor is not a token this
	// server minted, belongs to another tool, or the request is malformed.
	ErrorCodeInvalid querycontract.ErrorCode = "rank_cursor_invalid"
	// ErrorCodeUnsupported is HTTP 400: the request shape cannot be walked
	// in relevance order (see the Reason* constants for each case).
	ErrorCodeUnsupported querycontract.ErrorCode = "rank_cursor_unsupported"
	// ErrorCodeBasisMismatch is HTTP 409: the candidate set or the ranked
	// order changed since the cursor was minted, so continuing could repeat
	// or skip a row. The client restarts the walk with rank_cursor="start".
	ErrorCodeBasisMismatch querycontract.ErrorCode = "rank_cursor_basis_mismatch"
)

// Reasons carried in the error envelope's details.reason. Each is a closed,
// documented value.
const (
	// ReasonMalformed: the token failed to decode or its checksum failed.
	ReasonMalformed = "malformed"
	// ReasonToolMismatch: the token was minted by the other route.
	ReasonToolMismatch = "tool_mismatch"
	// ReasonInvalidLimit: the page limit is not positive.
	ReasonInvalidLimit = "invalid_limit"
	// ReasonCandidateSetChanged: the 200-row lexical window, its row
	// content, the query scope, or the ranker identity changed.
	ReasonCandidateSetChanged = "candidate_set_changed"
	// ReasonRankOrderChanged: the window is the same, but the rows already
	// served are no longer the top of this replica's order, for example
	// after a cross-architecture score flip.
	ReasonRankOrderChanged = "rank_order_changed"
	// ReasonRankerDisabled: the deployment has no hybrid ranker (the
	// semantic-search embedder runtime is off).
	ReasonRankerDisabled = "ranker_disabled"
	// ReasonExactSearch: find_code exact=true is an identity lookup and is
	// not ranked.
	ReasonExactSearch = "exact_search"
	// ReasonScopeNotSingleRepo: a walk needs exactly one repository.
	ReasonScopeNotSingleRepo = "scope_not_single_repo"
	// ReasonFileSearch: search_file_content does not support rank walks.
	ReasonFileSearch = "file_search"
	// ReasonOffsetConflict: rank_cursor was sent with a positive offset.
	ReasonOffsetConflict = "offset_conflict"
	// ReasonCursorConflict: rank_cursor was sent with the unscoped search
	// cursor.
	ReasonCursorConflict = "cursor_conflict"
)

var unsupportedMessages = map[string]string{
	ReasonRankerDisabled:     "relevance walks need the hybrid ranker, which is off in this deployment; page with offset instead",
	ReasonExactSearch:        "exact=true is an identity lookup and is not ranked; page with offset instead",
	ReasonScopeNotSingleRepo: "a relevance walk needs exactly one repository; set repo_id",
	ReasonFileSearch:         "search_file_content does not support rank_cursor; page with offset instead",
	ReasonOffsetConflict:     "rank_cursor and a positive offset are mutually exclusive",
	ReasonCursorConflict:     "rank_cursor and cursor are mutually exclusive",
}

// Error is a typed rank-walk failure. Status is the HTTP status the route
// answers with; Code and Reason are stable machine-readable values.
type Error struct {
	Code    querycontract.ErrorCode
	Status  int
	Reason  string
	Message string
}

// Error renders the code, reason, and message, so a client that did not
// negotiate the envelope still reads the typed values in the detail text.
func (e *Error) Error() string {
	return e.detail()
}

// detail is the client-facing text: the stable code, the reason, and a fixed
// message from this package. It never carries backend error text.
func (e *Error) detail() string {
	return string(e.Code) + ": " + e.Reason + ": " + e.Message
}

// Unsupported returns the HTTP 400 rank_cursor_unsupported error for reason.
func Unsupported(reason string) *Error {
	message, ok := unsupportedMessages[reason]
	if !ok {
		message = "this request cannot be walked in relevance order"
	}
	return &Error{Code: ErrorCodeUnsupported, Status: http.StatusBadRequest, Reason: reason, Message: message}
}

func invalid(reason, message string) *Error {
	return &Error{Code: ErrorCodeInvalid, Status: http.StatusBadRequest, Reason: reason, Message: message}
}

func mismatch(reason, message string) *Error {
	return &Error{Code: ErrorCodeBasisMismatch, Status: http.StatusConflict, Reason: reason, Message: message}
}

// WriteError answers a request with err when err is a *Error and reports
// whether it did. The negotiated envelope carries details.reason, and for a
// basis mismatch or an invalid cursor details.restart_with="start". Any other
// error is left to the caller.
func WriteError(w http.ResponseWriter, r *http.Request, err error) bool {
	var rankErr *Error
	if err == nil || !errors.As(err, &rankErr) {
		return false
	}
	details := map[string]any{"reason": rankErr.Reason}
	if rankErr.Code != ErrorCodeUnsupported {
		details["restart_with"] = StartToken
	}
	querycontract.WriteErrorEnvelope(w, r, rankErr.Status, &querycontract.ErrorEnvelope{
		Code:    rankErr.Code,
		Message: rankErr.detail(),
		Details: details,
	})
	return true
}
