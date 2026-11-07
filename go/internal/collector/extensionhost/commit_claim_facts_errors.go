// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// classifyCommitError maps a commit failure to its protocol code and HTTP
// status. Oversize is terminal-invalid (the bytes can never shrink on
// retry); fenced-away epochs keep the plane's verbatim code with the 409
// shape the proxy copies back; anything else is a 500 whose detail stays in
// the error return, never on the wire.
func classifyCommitError(ctx context.Context, err error) (string, int) {
	if errors.Is(err, collector.ErrCommitClaimFactsTooLarge) {
		return workflowv1.CodeInvalid, http.StatusBadRequest
	}
	if code := workflow.ClaimPlaneErrorCode(err); code != "" {
		return code, statusForCode(code)
	}
	if errors.Is(err, workflow.ErrClaimRejected) {
		return workflowv1.CodeStaleFence, http.StatusConflict
	}
	if ctx.Err() != nil {
		return "internal", http.StatusInternalServerError
	}
	return "internal", http.StatusInternalServerError
}

// statusForCode mirrors the protocol status table for the codes a commit can
// carry: stale-fence and lease-expiry are conflicts (the caller holds an
// epoch the server no longer honors and must re-claim, never retry the same
// mutation), oversize is terminal-invalid, and paced asks for a later retry.
func statusForCode(code string) int {
	switch code {
	case workflowv1.CodeInvalid, workflowv1.CodeVersionMismatch:
		return http.StatusBadRequest
	case workflowv1.CodeNotFound:
		return http.StatusNotFound
	case workflowv1.CodeClaimConflict, workflowv1.CodeStaleFence, workflowv1.CodeLeaseExpired:
		return http.StatusConflict
	case workflowv1.CodePaced:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

// commitErrorMessage renders the safe wire detail. Plane and lane refusals
// already carry bounded envelope text (codes, counts, knob names — never
// request or document values), so they cross verbatim like the proxy does.
// Transport and store faults collapse to a fixed string so credentials,
// payloads, and paths never reach the caller.
func commitErrorMessage(err error) string {
	if workflow.ClaimPlaneErrorCode(err) != "" {
		return err.Error()
	}
	var proto *workflowv1.ProtocolError
	if errors.As(err, &proto) {
		return proto.Message
	}
	if errors.Is(err, collector.ErrCommitClaimFactsTooLarge) {
		return err.Error()
	}
	if errors.Is(err, workflow.ErrClaimRejected) {
		return "claim rejected: the epoch is no longer live"
	}
	return "internal error"
}

// asProtocolError unwraps a coded protocol failure from the admission path.
func asProtocolError(err error) (*workflowv1.ProtocolError, bool) {
	var proto *workflowv1.ProtocolError
	if errors.As(err, &proto) {
		return proto, true
	}
	return nil, false
}

// writeCommitError answers one workflow.v1 error envelope: the code is the
// stable contract, the message is human detail no caller may parse.
func writeCommitError(writer http.ResponseWriter, status int, code, message string) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{"code": code, "message": message})
}
