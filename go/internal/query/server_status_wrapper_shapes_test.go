// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"
)

// queryWrapperShapePrelude is shared by the wrapper shapes: a renamed JSON
// writer that sets the status with WriteHeader and encodes the body, and an
// error writer that forwards status and message to it, as
// compare/handler.go's writeCompareError did.
const queryWrapperShapePrelude = `package p

import "net/http"

func writeJ(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeE(w http.ResponseWriter, status int, message string) {
	writeJ(w, status, map[string]any{"error": http.StatusText(status), "detail": message})
}
`

// TestQueryWriterWrapperShapes is the seeded RED/GREEN proof that both guard
// assertions follow writers through renamed wrappers (queryGuardTree). Each
// case appends declarations to queryWrapperShapePrelude and counts the sites
// assertion A (wantA) and assertion B (wantB) find in the whole file, the
// wrapper bodies included.
func TestQueryWriterWrapperShapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		decls        string
		wantA, wantB int
	}{
		// Caught through a status-and-message forwarder chain.
		{"chain_500_fixed_text", `func h(w http.ResponseWriter) { writeE(w, http.StatusInternalServerError, "failed") }`, 0, 1},
		{"chain_500_err_text", `func h(w http.ResponseWriter, err error) { writeE(w, 500, err.Error()) }`, 1, 1},
		{"chain_503_err_text", `func h(w http.ResponseWriter, err error) { writeE(w, http.StatusServiceUnavailable, err.Error()) }`, 1, 0},
		{"chain_computed_status", `func h(w http.ResponseWriter, err error) { writeE(w, statusFor(err), "failed") }`, 0, 1},
		{"method_wrapper", `func (s *S) writeM(w http.ResponseWriter, status int) { writeJ(w, status, nil) }
func h(s *S, w http.ResponseWriter) { s.writeM(w, http.StatusBadGateway) }`, 0, 1},
		// A fixed-status writer is named at its own write and at every caller.
		{"fixed_wrapper", `func writeInternal(w http.ResponseWriter) { writeE(w, 500, "failed") }
func h(w http.ResponseWriter) { writeInternal(w) }`, 0, 2},
		{"fixed_wrapper_transitive", `func writeInternal(w http.ResponseWriter) { writeE(w, 500, "failed") }
func list(w http.ResponseWriter) { writeInternal(w) }
func h(w http.ResponseWriter) { list(w) }`, 0, 3},
		// Message through locals, and a writer that fixes its own status.
		{"message_through_locals", `func writeD(w http.ResponseWriter, status int, msg string) {
	body := map[string]any{}
	body["message"] = msg
	WriteJSON(w, status, body)
}
func h(w http.ResponseWriter, err error) { writeD(w, 503, err.Error()) }`, 1, 0},
		{"message_behind_fixed_500", `func writeF(w http.ResponseWriter, msg string) { WriteError(w, 500, msg) }
func h(w http.ResponseWriter, err error) { writeF(w, err.Error()) }`, 1, 2},
		{"raw_body_behind_500_header", `func writeRaw(w http.ResponseWriter, msg string) {
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(msg))
}
func h(w http.ResponseWriter, err error) { writeRaw(w, err.Error()) }`, 1, 2},

		// Clean.
		{"chain_400_err_text", `func h(w http.ResponseWriter, err error) { writeE(w, http.StatusBadRequest, err.Error()) }`, 0, 0},
		{"chain_200", `func h(w http.ResponseWriter) { writeJ(w, http.StatusOK, nil) }`, 0, 0},
		{"message_behind_fixed_400", `func writeBad(w http.ResponseWriter, msg string) { WriteError(w, http.StatusBadRequest, msg) }
func h(w http.ResponseWriter, err error) { writeBad(w, err.Error()) }`, 0, 0},
		{"zero_do_not_write_sentinel", `func issue(w http.ResponseWriter, status int) {
	if status > 0 {
		WriteJSON(w, status, nil)
	}
}
func h(w http.ResponseWriter) { issue(w, 0) }`, 0, 0},
		{"status_returner_seam", `func build(ctx context.Context, err error) (int, *ErrorEnvelope) {
	if err == nil {
		return http.StatusNotFound, nil
	}
	return tracing.ServerFailureEnvelope(ctx, err, "failed", "c")
}
func h(w http.ResponseWriter, r *http.Request, err error) {
	status, env := build(r.Context(), err)
	WriteErrorEnvelope(w, r, status, env)
}`, 0, 0},

		// Declared after their callers: only a fixed point finds these.
		{"forwarder_declared_after_its_wrapper", `func h(w http.ResponseWriter) { outer(w, http.StatusBadGateway) }
func outer(w http.ResponseWriter, status int) { inner(w, status) }
func inner(w http.ResponseWriter, status int) { WriteError(w, status, "failed") }`, 0, 1},
		{"fixed_chain_declared_in_reverse", `func h(w http.ResponseWriter) { a(w) }
func a(w http.ResponseWriter) { b(w) }
func b(w http.ResponseWriter) { c(w) }
func c(w http.ResponseWriter) { WriteError(w, http.StatusInternalServerError, "failed") }`, 0, 4},

		// Near misses that must still fail.
		{"returner_with_500_branch", `func build(err error) (int, *ErrorEnvelope) {
	if err == nil {
		return http.StatusInternalServerError, nil
	}
	return http.StatusNotFound, nil
}
func h(w http.ResponseWriter, r *http.Request, err error) {
	status, env := build(err)
	WriteErrorEnvelope(w, r, status, env)
}`, 0, 1},
		{"reassigning_forwarder_is_not_a_wrapper", `func writeC(w http.ResponseWriter, status int) {
	status = clamp(status)
	WriteJSON(w, status, nil)
}
func h(w http.ResponseWriter) { writeC(w, 500) }`, 0, 1},
		{"zero_at_base_sink", `func h(w http.ResponseWriter) { WriteJSON(w, 0, nil) }`, 0, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := queryWrapperShapePrelude + "\n" + tc.decls + "\n"
			tree, file := queryGuardTreeFromSource(t, tc.name+".go", src)
			if got := len(queryErrorTextLeakSites(tree, file)); got != tc.wantA {
				t.Errorf("assertion A found %d sites, want %d:\n%s\nsource:\n%s",
					got, tc.wantA, formatQueryErrorTextLeakSites(queryErrorTextLeakSites(tree, file)), src)
			}
			if got := len(queryDirectServerStatusSites(tree, file)); got != tc.wantB {
				t.Errorf("assertion B found %d sites, want %d:\n%s\nsource:\n%s",
					got, tc.wantB, formatQueryErrorTextLeakSites(queryDirectServerStatusSites(tree, file)), src)
			}
		})
	}
}
