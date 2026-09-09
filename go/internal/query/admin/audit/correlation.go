// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// AppendTimeout bounds one governance audit append from an admin handler.
// It mirrors governanceAuditAppendTimeout (internal/query/auth_audit.go): the
// audit write never blocks the action it records.
const AppendTimeout = 500 * time.Millisecond

// SafeCorrelationID sanitizes a caller-supplied correlation id for audit
// storage. Repointed from safeAuditCorrelationID
// (internal/query/auth_audit.go): blank or over-long values become empty,
// and anything outside [a-z0-9_:-] is rejected rather than stored.
func SafeCorrelationID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 96 {
		return ""
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') &&
			r != '_' && r != '-' && r != ':' {
			return ""
		}
	}
	return value
}

// CorrelationID resolves the audit correlation id for a request.
// Repointed from documentationCorrelationID
// (internal/query/documentation.go): the caller-supplied request headers
// win, otherwise a random hex id (wall-clock fallback when rand fails).
func CorrelationID(r *http.Request) string {
	for _, header := range []string{"X-Correlation-ID", "X-Request-ID"} {
		if value := strings.TrimSpace(r.Header.Get(header)); value != "" {
			return value
		}
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	}
	return hex.EncodeToString(raw[:])
}

// RequirePermissionFeature enforces the permission-catalog feature gate for
// an admin route. Repointed from requirePermissionFeature
// (internal/query/permission_catalog.go): the decision stays canonical in
// queryauth.AllowsPermissionFeature, and the denial envelope keeps the root
// shape through querycontract.
func RequirePermissionFeature(w http.ResponseWriter, r *http.Request, capability string, feature string) bool {
	if queryauth.AllowsPermissionFeature(r.Context(), feature) {
		return true
	}
	WritePermissionDeniedEnvelope(w, capability)
	return false
}

// WritePermissionDeniedEnvelope writes the stable permission-denied error
// envelope. Repointed from writePermissionDeniedEnvelope
// (internal/query/permission_catalog.go) through querycontract, which owns
// the envelope structs.
func WritePermissionDeniedEnvelope(w http.ResponseWriter, capability string) {
	querycontract.WriteJSON(w, http.StatusForbidden, querycontract.ResponseEnvelope{Error: &querycontract.ErrorEnvelope{
		Code:       querycontract.ErrorCodePermissionDenied,
		Message:    "permission denied",
		Capability: capability,
	}})
}

// AddOptionalTime adds a timestamp field only when it is set, so a never-set
// nullable column renders as absent rather than the zero time. Moved from
// addOptionalTime (admin identity audit file), which the identity reads and
// the provider-config reads both call.
func AddOptionalTime(row map[string]any, key string, value time.Time) {
	if !value.IsZero() {
		row[key] = value.UTC()
	}
}
