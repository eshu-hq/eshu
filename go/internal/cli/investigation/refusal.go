// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/cli/apierr"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
)

// RefusalFromErrorCode maps an in-envelope error code to a packet refusal state
// when one applies. Codes without a refusal mapping return false so the caller
// surfaces them as a CLI error instead of an artifact that looks like an answer.
func RefusalFromErrorCode(code querycontract.ErrorCode) (packet.PacketRefusalState, bool) {
	switch code {
	case querycontract.ErrorCodeNotFound, querycontract.ErrorCodeScopeNotFound, querycontract.ErrorCodeServiceNotFound:
		return packet.PacketRefusalScopeNotFound, true
	case querycontract.ErrorCodeUnsupportedCapability, querycontract.ErrorCodeCapabilityDegraded:
		return packet.PacketRefusalProfileUnsupported, true
	case querycontract.ErrorCodeBackendUnavailable, querycontract.ErrorCodeIndexBuilding:
		return packet.PacketRefusalBackendUnavailable, true
	default:
		return packet.PacketRefusalNone, false
	}
}

// RefusalFromFetchError maps a transport-level API error to a refusal state. A
// 404 becomes scope_not_found; a 503 becomes backend_unavailable. Other statuses
// are surfaced to the operator as a CLI error.
//
// Only the HTTP status decides. This family has no message inspection, which is
// the one place it differs from `eshu trace`: trace checks the error text for
// "connection refused" and "request failed" BEFORE any status switch, so a 400
// carrying that text classifies as backend_unavailable there. Here the same 400
// stays a CLI error, and TestRefusalFromFetchErrorClassifiesByStatusNotMessage
// pins that.
//
// apierr.StatusCode reports false for a nil error, so no nil guard is needed.
func RefusalFromFetchError(err error) (packet.PacketRefusalState, bool) {
	status, ok := apierr.StatusCode(err)
	if !ok {
		return packet.PacketRefusalNone, false
	}
	switch status {
	case 404:
		return packet.PacketRefusalScopeNotFound, true
	case 501:
		// The explain handler returns 501 for a profile that cannot serve the
		// capability; GetEnvelope surfaces it as a transport error before the
		// in-envelope unsupported_capability code can be read.
		return packet.PacketRefusalProfileUnsupported, true
	case 503:
		return packet.PacketRefusalBackendUnavailable, true
	default:
		return packet.PacketRefusalNone, false
	}
}

// RefusalFromEnvelopeError maps an in-envelope error to a refusal state, or to a
// CLI error when no refusal mapping applies. A nil error means no refusal.
//
// The CLI error names the code and message the API returned. That text goes to
// stderr and never into the artifact, which is what keeps server-supplied
// strings out of a share-safe packet.
func RefusalFromEnvelopeError(errEnv *querycontract.ErrorEnvelope) (packet.PacketRefusalState, bool, error) {
	if errEnv == nil {
		return packet.PacketRefusalNone, false, nil
	}
	if refusal, ok := RefusalFromErrorCode(errEnv.Code); ok {
		return refusal, true, nil
	}
	return packet.PacketRefusalNone, false, fmt.Errorf("read failed: %s: %s", errEnv.Code, errEnv.Message)
}

// refusalPacket builds a valid refusal artifact for a family and scope.
//
// The packet builder's error is returned unwrapped so the operator reads the
// contract's own message; a prefix here would change CLI output for no gain.
func refusalPacket(family packet.InvestigationFamily, subject map[string]string, refusal packet.PacketRefusalState) (packet.InvestigationEvidencePacket, error) {
	//nolint:wrapcheck // operator-facing text stays exactly as the contract wrote it
	return packet.NewInvestigationEvidencePacket(packet.InvestigationPacketInput{
		Family:  family,
		Subject: SubjectOrPlaceholder(subject),
		Refusal: refusal,
	})
}
