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
