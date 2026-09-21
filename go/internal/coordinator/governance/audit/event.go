// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

const (
	// AppendTimeout bounds one governance-audit append so a slow or wedged
	// audit store can never stall the reconcile or claim path that emitted
	// the event.
	AppendTimeout = 500 * time.Millisecond
	// ServiceID is the service-principal identity every coordinator-emitted
	// governance audit event is attributed to.
	ServiceID = "svc:workflow-coordinator"
)

// Hash renders parts as the redacted, validation-safe scope-id hash carried
// by governance audit events. Parts are trimmed and joined with a NUL
// separator so no part boundary can be forged by embedded text, and the
// result is prefixed with its digest algorithm.
func Hash(parts ...string) string {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized = append(normalized, strings.TrimSpace(part))
	}
	sum := sha256.Sum256([]byte(strings.Join(normalized, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CorrelationID renders a stable, redacted correlation id for the audit
// events one decision emits: the prefix names the decision family and the
// suffix is the leading 64 bits of [Hash] over parts.
func CorrelationID(prefix string, parts ...string) string {
	hash := strings.TrimPrefix(Hash(parts...), "sha256:")
	return strings.TrimSpace(prefix) + ":" + hash[:16]
}
