// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/parser"
	"go/token"
	"testing"
)

// TestQueryErrorTextLeakSitesShapes is the seeded RED/GREEN proof for
// queryErrorTextLeakSites, the walker behind
// TestNoNewQueryErrorTextLeakSites (#7674). Each "caught" case plants one leak
// site and must count exactly 1; each "clean" case must count 0. The "gap"
// cases pin shapes the walker deliberately cannot see without type
// resolution, so a future change that starts catching them updates this table
// on purpose rather than by accident.
func TestQueryErrorTextLeakSitesShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want int
	}{
		// Caught: a server status with error text in the body.
		{"500_err_Error", `querycontract.WriteError(w, http.StatusInternalServerError, err.Error())`, 1},
		{"500_sprintf_err", `querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("query failed: %v", err))`, 1},
		{"500_sprintf_suffix_err", `querycontract.WriteError(w, http.StatusInternalServerError, fmt.Sprintf("q: %v", configErr))`, 1},
		{"500_errorf_err", `querycontract.WriteError(w, http.StatusInternalServerError, fmt.Errorf("q: %w", rerr).Error())`, 1},
		{"503_err_Error", `querycontract.WriteError(w, http.StatusServiceUnavailable, err.Error())`, 1},
		{"computed_status_err_Error", `querycontract.WriteError(w, statusFor(err), err.Error())`, 1},
		{"concat_err_Error", `WriteError(w, http.StatusInternalServerError, "x: "+err.Error())`, 1},
		{"int_500_err_Error", `WriteError(w, 500, err.Error())`, 1},
		{"method_WriteError", `x.WriteError(w, http.StatusInternalServerError, err.Error())`, 1},
		{"nested_method_WriteError", `a.deps.WriteError(w, http.StatusBadGateway, err.Error())`, 1},
		{"WriteContractError", `querycontract.WriteContractError(w, r, http.StatusInternalServerError, err.Error(), code, "c", p, p)`, 1},
		{"writeContractError_method", `h.writeContractError(w, r, http.StatusInternalServerError, err.Error(), code)`, 1},
		{"http_Error", `http.Error(w, err.Error(), http.StatusInternalServerError)`, 1},
		{"envelope_literal_no_status", `env := &querycontract.ErrorEnvelope{Code: c, Message: err.Error()}; _ = env`, 1},
		{"envelope_literal_503", `querycontract.WriteErrorEnvelope(w, r, http.StatusServiceUnavailable, &querycontract.ErrorEnvelope{Message: err.Error()})`, 1},
		{"envelope_literal_bare_type", `_ = ErrorEnvelope{Message: fmt.Sprintf("q: %v", err)}`, 1},

		// Clean: fixed text, a 4xx status, or the #7672 helper.
		{"helper_WriteServerFailure", `tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, msg)`, 0},
		{"400_err_Error", `querycontract.WriteError(w, http.StatusBadRequest, err.Error())`, 0},
		{"404_err_Error", `WriteError(w, http.StatusNotFound, err.Error())`, 0},
		{"409_err_Error", `WriteError(w, http.StatusConflict, err.Error())`, 0},
		{"int_422_err_Error", `WriteError(w, 422, err.Error())`, 0},
		{"500_fixed", `WriteError(w, 500, "fixed")`, 0},
		{"500_fixed_const", `WriteError(w, http.StatusInternalServerError, queryFailedMessage)`, 0},
		{"503_sentinel_Error", `WriteError(w, http.StatusServiceUnavailable, errGraphUnavailable.Error())`, 0},
		{"503_qualified_sentinel_Error", `WriteError(w, http.StatusServiceUnavailable, querycontract.ErrReaderRetryable.Error())`, 0},
		{"http_Error_400", `http.Error(w, err.Error(), http.StatusBadRequest)`, 0},
		{"envelope_literal_400_call", `querycontract.WriteErrorEnvelope(w, r, http.StatusBadRequest, &querycontract.ErrorEnvelope{Message: err.Error()})`, 0},
		{"envelope_literal_409_return", `return http.StatusConflict, &querycontract.ErrorEnvelope{Message: ambiguous.Error()}`, 0},
		{"envelope_literal_fixed", `_ = &querycontract.ErrorEnvelope{Message: "fixed"}`, 0},
		{"other_writer_name", `writeProviderConfigWriteError(w, err)`, 0},

		// Known gaps: not caught without type resolution.
		{"gap_local_message_var", `msg := err.Error(); WriteError(w, 500, msg)`, 0},
		{"gap_wrapper_helper", `writeLeaky(w, err)`, 0},
		{"gap_sentinel_named_local", `errResp := do(); WriteError(w, 500, errResp.Error())`, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "package p\n\nfunc f() (int, any) {\n" + tc.body + "\nreturn 0, nil\n}\n"
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tc.name+".go", src, 0)
			if err != nil {
				t.Fatalf("parse synthetic source: %v\n%s", err, src)
			}
			if got := len(queryErrorTextLeakSites(fset, file)); got != tc.want {
				t.Fatalf("queryErrorTextLeakSites() found %d sites, want %d\nbody: %s", got, tc.want, tc.body)
			}
		})
	}
}
