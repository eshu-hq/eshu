// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type writeErrorCase struct {
	name string
	err  *Error
}

// writeErrorCases builds one error per constructor call this package makes,
// with the exact reason and message of that call site, plus the Unsupported
// fallback for an unlisted reason.
func writeErrorCases() []writeErrorCase {
	return []writeErrorCase{
		{"invalid malformed token", invalid(ReasonMalformed, "rank_cursor is not a cursor this server returned; restart with rank_cursor=\"start\"")},
		{"invalid advanced past window", invalid(ReasonMalformed, "rank_cursor advanced past the ranked window")},
		{"invalid repeated row id", invalid(ReasonMalformed, "rank_cursor cannot advance over a repeated or empty row id")},
		{"invalid limit", invalid(ReasonInvalidLimit, "limit must be positive")},
		{"invalid tool mismatch", invalid(ReasonToolMismatch, "rank_cursor was returned by another tool")},
		{"invalid past end", invalid(ReasonMalformed, "rank_cursor is past the end of the ranked window")},
		{"mismatch lane changed", mismatch(ReasonBasisChanged, "the search lane changed since this cursor was returned; restart with rank_cursor=\"start\"")},
		{"mismatch window changed", mismatch(ReasonBasisChanged, "the ranked window changed since this cursor was returned; restart with rank_cursor=\"start\"")},
		{"mismatch order changed", mismatch(ReasonOrderChanged, "the ranked order changed since this cursor was returned; restart with rank_cursor=\"start\"")},
		{"unsupported ranker disabled", Unsupported(ReasonRankerDisabled)},
		{"unsupported scope not single repo", Unsupported(ReasonScopeNotSingleRepo)},
		{"unsupported file search", Unsupported(ReasonFileSearch)},
		{"unsupported offset conflict", Unsupported(ReasonOffsetConflict)},
		{"unsupported cursor conflict", Unsupported(ReasonCursorConflict)},
		{"unsupported unlisted reason", Unsupported("unlisted")},
	}
}

// writeErrorGolden is what WriteError answered for each case before #7674
// slice 14 replaced the struct-field status operand with literal 400/409
// writes, captured from the unchanged code: the status and the exact
// envelope and plain body bytes. The refactor must leave every one unchanged.
var writeErrorGolden = map[string]struct {
	status          int
	envelope, plain string
}{
	"invalid malformed token": {
		status:   400,
		envelope: `{"data":null,"truth":null,"error":{"code":"rank_cursor_invalid","message":"rank_cursor_invalid: malformed: rank_cursor is not a cursor this server returned; restart with rank_cursor=\"start\"","details":{"reason":"malformed","restart_with":"start"}}}` + "\n",
		plain:    `{"detail":"rank_cursor_invalid: malformed: rank_cursor is not a cursor this server returned; restart with rank_cursor=\"start\"","error":"Bad Request"}` + "\n",
	},
	"invalid advanced past window": {
		status:   400,
		envelope: `{"data":null,"truth":null,"error":{"code":"rank_cursor_invalid","message":"rank_cursor_invalid: malformed: rank_cursor advanced past the ranked window","details":{"reason":"malformed","restart_with":"start"}}}` + "\n",
		plain:    `{"detail":"rank_cursor_invalid: malformed: rank_cursor advanced past the ranked window","error":"Bad Request"}` + "\n",
	},
	"invalid repeated row id": {
		status:   400,
		envelope: `{"data":null,"truth":null,"error":{"code":"rank_cursor_invalid","message":"rank_cursor_invalid: malformed: rank_cursor cannot advance over a repeated or empty row id","details":{"reason":"malformed","restart_with":"start"}}}` + "\n",
		plain:    `{"detail":"rank_cursor_invalid: malformed: rank_cursor cannot advance over a repeated or empty row id","error":"Bad Request"}` + "\n",
	},
	"invalid limit": {
		status:   400,
		envelope: `{"data":null,"truth":null,"error":{"code":"rank_cursor_invalid","message":"rank_cursor_invalid: invalid_limit: limit must be positive","details":{"reason":"invalid_limit","restart_with":"start"}}}` + "\n",
		plain:    `{"detail":"rank_cursor_invalid: invalid_limit: limit must be positive","error":"Bad Request"}` + "\n",
	},
	"invalid tool mismatch": {
		status:   400,
		envelope: `{"data":null,"truth":null,"error":{"code":"rank_cursor_invalid","message":"rank_cursor_invalid: tool_mismatch: rank_cursor was returned by another tool","details":{"reason":"tool_mismatch","restart_with":"start"}}}` + "\n",
		plain:    `{"detail":"rank_cursor_invalid: tool_mismatch: rank_cursor was returned by another tool","error":"Bad Request"}` + "\n",
	},
	"invalid past end": {
		status:   400,
		envelope: `{"data":null,"truth":null,"error":{"code":"rank_cursor_invalid","message":"rank_cursor_invalid: malformed: rank_cursor is past the end of the ranked window","details":{"reason":"malformed","restart_with":"start"}}}` + "\n",
		plain:    `{"detail":"rank_cursor_invalid: malformed: rank_cursor is past the end of the ranked window","error":"Bad Request"}` + "\n",
	},
	"mismatch lane changed": {
		status:   409,
		envelope: `{"data":null,"truth":null,"error":{"code":"rank_cursor_basis_mismatch","message":"rank_cursor_basis_mismatch: basis_changed: the search lane changed since this cursor was returned; restart with rank_cursor=\"start\"","details":{"reason":"basis_changed","restart_with":"start"}}}` + "\n",
		plain:    `{"detail":"rank_cursor_basis_mismatch: basis_changed: the search lane changed since this cursor was returned; restart with rank_cursor=\"start\"","error":"Conflict"}` + "\n",
	},
	"mismatch window changed": {
		status:   409,
		envelope: `{"data":null,"truth":null,"error":{"code":"rank_cursor_basis_mismatch","message":"rank_cursor_basis_mismatch: basis_changed: the ranked window changed since this cursor was returned; restart with rank_cursor=\"start\"","details":{"reason":"basis_changed","restart_with":"start"}}}` + "\n",
		plain:    `{"detail":"rank_cursor_basis_mismatch: basis_changed: the ranked window changed since this cursor was returned; restart with rank_cursor=\"start\"","error":"Conflict"}` + "\n",
	},
	"mismatch order changed": {
		status:   409,
		envelope: `{"data":null,"truth":null,"error":{"code":"rank_cursor_basis_mismatch","message":"rank_cursor_basis_mismatch: order_changed: the ranked order changed since this cursor was returned; restart with rank_cursor=\"start\"","details":{"reason":"order_changed","restart_with":"start"}}}` + "\n",
		plain:    `{"detail":"rank_cursor_basis_mismatch: order_changed: the ranked order changed since this cursor was returned; restart with rank_cursor=\"start\"","error":"Conflict"}` + "\n",
	},
	"unsupported ranker disabled": {
		status:   400,
		envelope: `{"data":null,"truth":null,"error":{"code":"rank_cursor_unsupported","message":"rank_cursor_unsupported: ranker_disabled: search_entity_content walks need the hybrid ranker, which is off in this deployment; page with offset instead","details":{"reason":"ranker_disabled"}}}` + "\n",
		plain:    `{"detail":"rank_cursor_unsupported: ranker_disabled: search_entity_content walks need the hybrid ranker, which is off in this deployment; page with offset instead","error":"Bad Request"}` + "\n",
	},
	"unsupported scope not single repo": {
		status:   400,
		envelope: `{"data":null,"truth":null,"error":{"code":"rank_cursor_unsupported","message":"rank_cursor_unsupported: scope_not_single_repo: a relevance walk needs exactly one repository; set repo_id","details":{"reason":"scope_not_single_repo"}}}` + "\n",
		plain:    `{"detail":"rank_cursor_unsupported: scope_not_single_repo: a relevance walk needs exactly one repository; set repo_id","error":"Bad Request"}` + "\n",
	},
	"unsupported file search": {
		status:   400,
		envelope: `{"data":null,"truth":null,"error":{"code":"rank_cursor_unsupported","message":"rank_cursor_unsupported: file_search: search_file_content does not support rank_cursor; page with offset instead","details":{"reason":"file_search"}}}` + "\n",
		plain:    `{"detail":"rank_cursor_unsupported: file_search: search_file_content does not support rank_cursor; page with offset instead","error":"Bad Request"}` + "\n",
	},
	"unsupported offset conflict": {
		status:   400,
		envelope: `{"data":null,"truth":null,"error":{"code":"rank_cursor_unsupported","message":"rank_cursor_unsupported: offset_conflict: rank_cursor and a positive offset are mutually exclusive","details":{"reason":"offset_conflict"}}}` + "\n",
		plain:    `{"detail":"rank_cursor_unsupported: offset_conflict: rank_cursor and a positive offset are mutually exclusive","error":"Bad Request"}` + "\n",
	},
	"unsupported cursor conflict": {
		status:   400,
		envelope: `{"data":null,"truth":null,"error":{"code":"rank_cursor_unsupported","message":"rank_cursor_unsupported: cursor_conflict: rank_cursor and cursor are mutually exclusive","details":{"reason":"cursor_conflict"}}}` + "\n",
		plain:    `{"detail":"rank_cursor_unsupported: cursor_conflict: rank_cursor and cursor are mutually exclusive","error":"Bad Request"}` + "\n",
	},
	"unsupported unlisted reason": {
		status:   400,
		envelope: `{"data":null,"truth":null,"error":{"code":"rank_cursor_unsupported","message":"rank_cursor_unsupported: unlisted: this request cannot be walked in relevance order","details":{"reason":"unlisted"}}}` + "\n",
		plain:    `{"detail":"rank_cursor_unsupported: unlisted: this request cannot be walked in relevance order","error":"Bad Request"}` + "\n",
	},
}

// TestWriteErrorIsByteForByteUnchanged pins every constructor call's code,
// status, and body. WriteError picks its status from the error code with a
// literal 400 or 409, not from the Status field (#7674 slice 14), so this
// test also requires the Status field each constructor sets to equal the
// status WriteError answers: the two can never drift apart.
func TestWriteErrorIsByteForByteUnchanged(t *testing.T) {
	t.Parallel()

	cases := writeErrorCases()
	if len(cases) != len(writeErrorGolden) {
		t.Fatalf("cases = %d, golden rows = %d", len(cases), len(writeErrorGolden))
	}
	for _, tc := range cases {
		want, ok := writeErrorGolden[tc.name]
		if !ok {
			t.Errorf("%s: no golden row", tc.name)
			continue
		}
		if tc.err.Status != want.status {
			t.Errorf("%s: Status field = %d, WriteError answers %d", tc.name, tc.err.Status, want.status)
		}
		for _, envelope := range []bool{true, false} {
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/search", nil)
			wantBody := want.plain
			if envelope {
				req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
				wantBody = want.envelope
			}
			rec := httptest.NewRecorder()
			if !WriteError(rec, req, fmt.Errorf("wrapped: %w", tc.err)) {
				t.Errorf("%s envelope=%v: WriteError returned false", tc.name, envelope)
				continue
			}
			if rec.Code != want.status {
				t.Errorf("%s envelope=%v: status = %d, want %d", tc.name, envelope, rec.Code, want.status)
			}
			if got := rec.Body.String(); got != wantBody {
				t.Errorf("%s envelope=%v: body =\n%s\nwant\n%s", tc.name, envelope, got, wantBody)
			}
			if got := rec.Header().Get("Retry-After"); got != "" {
				t.Errorf("%s envelope=%v: Retry-After = %q, want none", tc.name, envelope, got)
			}
		}
	}
}

// TestWriteErrorLeavesAnUnknownCodeToTheCaller pins the one input the code
// switch does not map: a hand-built *Error with a code outside ErrorCodes.
// No constructor makes one. WriteError writes nothing and returns false, so
// the caller answers it through its own failure path.
func TestWriteErrorLeavesAnUnknownCodeToTheCaller(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/code/search", nil)
	err := &Error{Code: "rank_cursor_other", Status: http.StatusInternalServerError, Reason: "x", Message: "y"}
	if WriteError(rec, req, err) {
		t.Fatal("WriteError handled an unknown code")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("WriteError wrote %q for an unknown code", rec.Body.String())
	}
}
