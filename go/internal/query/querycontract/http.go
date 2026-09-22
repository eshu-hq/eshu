// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// EnvelopeMIMEType selects the stable Eshu response envelope.
const EnvelopeMIMEType = "application/eshu.envelope+json"

// AcceptsEnvelope reports whether a request negotiated the Eshu envelope.
func AcceptsEnvelope(r *http.Request) bool {
	return r != nil && strings.Contains(r.Header.Get("Accept"), EnvelopeMIMEType)
}

// WriteJSON writes one JSON response.
func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value)
}

// WriteError writes a plain JSON error response.
func WriteError(w http.ResponseWriter, status int, message string) {
	WriteJSON(w, status, map[string]any{"error": http.StatusText(status), "detail": message})
}

// WriteSuccess writes either an envelope or the plain data payload.
func WriteSuccess(w http.ResponseWriter, r *http.Request, status int, data any, truth *TruthEnvelope) {
	if AcceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{Data: data, Truth: truth})
		return
	}
	WriteJSON(w, status, data)
}

// WriteErrorEnvelope writes either a canonical envelope or a plain error.
func WriteErrorEnvelope(w http.ResponseWriter, r *http.Request, status int, errEnv *ErrorEnvelope) {
	if errEnv == nil {
		WriteError(w, status, http.StatusText(status))
		return
	}
	if AcceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{Error: errEnv})
		return
	}
	WriteError(w, status, errEnv.Message)
}

// WriteContractError writes a capability/profile error using negotiated shape.
func WriteContractError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	message string,
	errCode ErrorCode,
	capability string,
	currentProfile QueryProfile,
	requiredProfile QueryProfile,
) {
	if AcceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{Error: &ErrorEnvelope{
			Code: errCode, Message: message, Capability: capability,
			Profiles: &ErrorProfiles{Current: currentProfile, Required: requiredProfile},
		}})
		return
	}
	WriteError(w, status, message)
}

// ReadJSON decodes one request body and closes it.
func ReadJSON(r *http.Request, value any) error {
	if r.Body == nil {
		return fmt.Errorf("request body is required")
	}
	defer func() { _ = r.Body.Close() }()
	if err := json.NewDecoder(r.Body).Decode(value); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

// QueryParam returns one trimmed query parameter.
func QueryParam(r *http.Request, key string) string {
	return strings.TrimSpace(r.URL.Query().Get(key))
}

// QueryParamInt returns one integer query parameter or defaultValue.
func QueryParamInt(r *http.Request, key string, defaultValue int) int {
	raw := QueryParam(r, key)
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	return value
}

// PathParam returns one trimmed ServeMux path value.
func PathParam(r *http.Request, name string) string {
	return strings.TrimSpace(r.PathValue(name))
}

// ParseBoundedLimit reads the limit query param, applying the default when
// blank and rejecting values outside [1, max]. It lives here (not in a
// handler family) so the repository routes and the surface-inventory stayer
// share one bound without importing each other (#6060, lane B B3).
func ParseBoundedLimit(w http.ResponseWriter, r *http.Request, def, max int) (int, bool) {
	raw := QueryParam(r, "limit")
	if raw == "" {
		return def, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > max {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf("limit must be an integer in [1, %d]", max))
		return 0, false
	}
	return limit, true
}

// CapabilityUnsupported reports whether profile has no truth ceiling for capability.
func CapabilityUnsupported(profile QueryProfile, capability string) bool {
	return maxTruthLevel(capability, profile) == nil
}

// RequiredProfile returns the minimum profile registered for capability.
func RequiredProfile(capability string) QueryProfile {
	support, ok := CapabilitySupportFor(capability)
	if !ok || support.RequiredProfile == "" {
		return ProfileLocalFullStack
	}
	return support.RequiredProfile
}

// ParseOffset reads the offset query param, defaulting to 0 and rejecting a
// negative or non-numeric value with 400. It is the pagination twin of
// ParseBoundedLimit and lives here for the same reason: every handler family
// that paginates needs it, so it cannot belong to any one of them (#6642).
func ParseOffset(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := QueryParam(r, "offset")
	if raw == "" {
		return 0, true
	}
	offset, err := strconv.Atoi(raw)
	if err != nil || offset < 0 {
		WriteError(w, http.StatusBadRequest, "offset must be a non-negative integer")
		return 0, false
	}
	return offset, true
}

// NextOffset returns the offset a caller should request next, or nil when the
// page was not truncated. Returning any rather than *int keeps it directly
// assignable to a JSON response field that must serialize as null.
func NextOffset(offset, limit int, truncated bool) any {
	if !truncated {
		return nil
	}
	return offset + limit
}

// ParseCatalogView reads the view query param, which selects between the
// compact and full projection of a catalog response. Blank means compact.
func ParseCatalogView(w http.ResponseWriter, r *http.Request) (full bool, ok bool) {
	switch raw := QueryParam(r, "view"); raw {
	case "", "compact":
		return false, true
	case "full":
		return true, true
	default:
		WriteError(w, http.StatusBadRequest, "view must be compact or full")
		return false, false
	}
}
