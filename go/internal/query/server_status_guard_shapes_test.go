// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

// TestQueryDirectServerStatusSitesShapes is the seeded RED/GREEN proof for
// queryDirectServerStatusSites, the walker behind
// TestNoDirectServerStatusWrites (#7674 assertion B). Each "caught" case plants
// one direct server-status write and must count exactly 1; each "clean" case
// must count 0. The "gap" cases pin writes the walker deliberately does not
// see, so a change that starts catching them updates this table on purpose.
//
// A body that does not start with "func " runs inside func f(); a body that
// does is a whole declaration, for the parameter-forwarding cases.
func TestQueryDirectServerStatusSitesShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want int
	}{
		// Caught: a server status, or a status the walker cannot prove allowed.
		{"500_const", `WriteError(w, http.StatusInternalServerError, "fixed")`, 1},
		{"500_int", `WriteError(w, 500, msg)`, 1},
		{"502_method", `x.WriteError(w, http.StatusBadGateway, msg)`, 1},
		{"504_nested_method", `a.deps.WriteError(w, http.StatusGatewayTimeout, msg)`, 1},
		{"408_const", `querycontract.WriteError(w, http.StatusRequestTimeout, msg)`, 1},
		{"408_int", `WriteError(w, 408, msg)`, 1},
		{"WriteContractError_500", `querycontract.WriteContractError(w, r, http.StatusInternalServerError, msg, code, "c", p, p)`, 1},
		{"writeContractError_502", `h.writeContractError(w, r, 502, msg, code)`, 1},
		{"http_Error_500", `http.Error(w, "fixed", http.StatusInternalServerError)`, 1},
		{"http_Error_unknown_const", `http.Error(w, "fixed", http.StatusHTTPVersionNotSupported)`, 1},
		{"local_var_disallowed_literal", `status := http.StatusOK; if bad { status = http.StatusBadGateway }; WriteJSON(w, status, body)`, 1},
		{"local_var_from_call", `status := statusFor(err); WriteError(w, status, msg)`, 1},
		{"local_var_declared_only", `var status int; WriteError(w, status, msg)`, 1},
		{"verdict_local_reassigned", `status, env := tracing.ServerFailureEnvelope(ctx, err, m, c); status = http.StatusInternalServerError; WriteErrorEnvelope(w, r, status, env)`, 1},
		{"verdict_local_other_call", `status, env := buildFailure(err); WriteErrorEnvelope(w, r, status, env)`, 1},
		{"verdict_local_wrong_position", `env, status := tracing.ServerFailureEnvelope(ctx, err, m, c); WriteErrorEnvelope(w, r, status, env)`, 1},
		{"WriteJSON_500", `querycontract.WriteJSON(w, http.StatusInternalServerError, body)`, 1},
		{"WriteErrorEnvelope_504", `WriteErrorEnvelope(w, r, http.StatusGatewayTimeout, env)`, 1},
		{"WriteSuccess_500", `WriteSuccess(w, r, 500, data, truth)`, 1},
		{"WriteHeader_500", `w.WriteHeader(http.StatusInternalServerError)`, 1},
		{"WriteHeader_zero", `w.WriteHeader(0)`, 1},
		{"field_status", `querycontract.WriteErrorEnvelope(w, r, rankErr.Status, env)`, 1},
		{"named_const_status", `WriteError(w, statusUnavailable, msg)`, 1},
		{"computed_status", `WriteError(w, statusFor(err), msg)`, 1},
		{"qualified_499_const", `WriteError(w, querycontract.StatusClientClosedRequest, msg)`, 1},
		{"local_message_var_500", `msg := err.Error(); WriteError(w, 500, msg)`, 1},
		{"param_reassigned", `func f(w http.ResponseWriter, status int) { status = 500; WriteError(w, status, "m") }`, 1},
		{"param_shadowed", `func f(w http.ResponseWriter, status int) { if true { status := 500; WriteError(w, status, "m") } }`, 1},
		{"param_incremented", `func f(w http.ResponseWriter, status int) { status++; WriteError(w, status, "m") }`, 1},
		{"param_var_redeclared", `func f(w http.ResponseWriter, status int) { { var status = 500; WriteError(w, status, "m") } }`, 1},
		{"closure_captures_outer_param", `func f(w http.ResponseWriter, status int) { g := func() { WriteError(w, status, "m") }; g() }`, 1},

		// Clean: an allowed literal, a pure forwarder, or a non-guarded call.
		{"400_const", `WriteError(w, http.StatusBadRequest, err.Error())`, 0},
		{"404_const", `x.WriteError(w, http.StatusNotFound, msg)`, 0},
		{"409_contract", `querycontract.WriteContractError(w, r, http.StatusConflict, msg, code, "c", p, p)`, 0},
		{"422_int", `WriteError(w, 422, msg)`, 0},
		{"499_int", `WriteError(w, 499, msg)`, 0},
		{"501_const", `WriteError(w, http.StatusNotImplemented, msg)`, 0},
		{"501_int", `h.writeContractError(w, r, 501, msg, code)`, 0},
		{"503_const", `WriteError(w, http.StatusServiceUnavailable, errGraphUnavailable.Error())`, 0},
		{"503_int", `http.Error(w, "fixed", 503)`, 0},
		{"pure_forwarder", `func f(w http.ResponseWriter, status int, m string) { WriteError(w, status, m) }`, 0},
		{"pure_forwarder_method", `func (h *H) f(w http.ResponseWriter, r *http.Request, status int) { h.writeContractError(w, r, status, "m", code) }`, 0},
		{"pure_forwarder_literal", `func f(w http.ResponseWriter) { g := func(status int) { WriteError(w, status, "m") }; g(400) }`, 0},
		{"helper_WriteServerFailure", `tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, msg)`, 0},
		{"too_few_args", `WriteError(w)`, 0},
		{"200_WriteJSON", `WriteJSON(w, http.StatusOK, body)`, 0},
		{"204_WriteHeader", `w.WriteHeader(http.StatusNoContent)`, 0},
		{"local_var_allowed_literals", `status := http.StatusCreated; if same { status = http.StatusOK }; WriteJSON(w, status, body)`, 0},
		{"local_var_503", `status := http.StatusServiceUnavailable; WriteError(w, status, msg)`, 0},
		{"verdict_local_server_failure", `status, env := tracing.ServerFailureEnvelope(ctx, err, m, c); WriteErrorEnvelope(w, r, status, env)`, 0},
		{"verdict_local_graph_read", `status, env, ok := querycontract.GraphReadErrorEnvelope(err, c); if ok { WriteErrorEnvelope(w, r, status, env) }`, 0},

		// Known gap: a callee declared nowhere in the tree (a function value,
		// an interface method, or another package's method) is not a writer.
		{"gap_unresolved_callee", `writeFailure(w, http.StatusInternalServerError)`, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decl := tc.body
			if !strings.HasPrefix(decl, "func ") {
				decl = "func f() {\n" + tc.body + "\n}"
			}
			src := "package p\n\n" + decl + "\n"
			tree, file := queryGuardTreeFromSource(t, tc.name+".go", src)
			if got := len(queryDirectServerStatusSites(tree, file)); got != tc.want {
				t.Fatalf("queryDirectServerStatusSites() found %d sites, want %d\nsource: %s", got, tc.want, src)
			}
		})
	}
}

// TestQueryDirectServerStatusExempt pins the one exempt package: only paths
// inside go/internal/query/tracing skip assertion B.
func TestQueryDirectServerStatusExempt(t *testing.T) {
	t.Parallel()

	for rel, want := range map[string]bool{
		"tracing/server_failure.go":       true,
		"tracing/sub/helper.go":           true,
		"tracingx/server_failure.go":      false,
		"admin/tracing/server_failure.go": false,
		"handler_tracing.go":              false,
		"querycontract/http.go":           false,
	} {
		if got := queryDirectServerStatusExempt(rel); got != want {
			t.Errorf("queryDirectServerStatusExempt(%q) = %t, want %t", rel, got, want)
		}
	}
}
