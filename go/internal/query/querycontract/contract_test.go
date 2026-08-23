// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCapabilityProfileAndTruthContracts(t *testing.T) {
	isolateCapabilityRegistry(t)

	exact := TruthLevelExact
	derived := TruthLevelDerived
	RegisterCapabilities(CapabilityRegistration{
		Capability: "test.profile_truth",
		Support: CapabilitySupport{
			LocalLightweightMax:   nil,
			LocalAuthoritativeMax: &derived,
			LocalFullStackMax:     &exact,
			ProductionMax:         &exact,
			RequiredProfile:       ProfileLocalAuthoritative,
		},
	})

	if !CapabilityUnsupported(ProfileLocalLightweight, "test.profile_truth") {
		t.Fatal("local lightweight profile is supported, want unsupported")
	}
	if got := RequiredProfile("test.profile_truth"); got != ProfileLocalAuthoritative {
		t.Fatalf("required profile = %q, want %q", got, ProfileLocalAuthoritative)
	}
	truth := BuildTruthEnvelope(ProfileLocalAuthoritative, "test.profile_truth", TruthBasisAuthoritativeGraph, "test")
	if got := truth.Level; got != TruthLevelDerived {
		t.Fatalf("truth level = %q, want %q ceiling", got, TruthLevelDerived)
	}
	if got := RequiredProfile("missing.capability"); got != ProfileLocalFullStack {
		t.Fatalf("unknown required profile = %q, want %q", got, ProfileLocalFullStack)
	}
	if !CapabilityUnsupported(ProfileProduction, "missing.capability") {
		t.Fatal("unknown capability is supported, want unsupported")
	}
}

func TestBuildTruthEnvelopePreservesUnknownCapabilityPanic(t *testing.T) {
	defer func() {
		if got := recover(); got != `query capability "missing.capability" missing from capability matrix` {
			t.Fatalf("panic = %v, want stable unknown-capability text", got)
		}
	}()
	_ = BuildTruthEnvelope(ProfileProduction, "missing.capability", TruthBasisHybrid, "test")
}

func TestWriteSuccessPreservesEnvelopeNegotiation(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	WriteSuccess(rec, req, http.StatusOK, map[string]string{"status": "ok"}, &TruthEnvelope{Level: TruthLevelExact})

	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response envelope: %v", err)
	}
	if envelope.Truth == nil || envelope.Truth.Level != TruthLevelExact {
		t.Fatalf("truth = %+v, want exact envelope", envelope.Truth)
	}
}

func TestRegistrationTracksDuplicatesAndLowLevelSetterStillOverwrites(t *testing.T) {
	isolateCapabilityRegistry(t)

	first := CapabilitySupport{RequiredProfile: ProfileLocalAuthoritative}
	second := CapabilitySupport{RequiredProfile: ProfileProduction}
	RegisterCapabilities(CapabilityRegistration{Capability: "test.duplicate", Support: first})
	RegisterCapabilities(CapabilityRegistration{Capability: "test.duplicate", Support: second})
	if duplicates := DuplicateCapabilityRegistrations(); len(duplicates) != 1 || duplicates[0] != "test.duplicate" {
		t.Fatalf("duplicates = %v, want [test.duplicate]", duplicates)
	}

	SetCapabilitySupport("test.duplicate", first)
	if got := RequiredProfile("test.duplicate"); got != ProfileLocalAuthoritative {
		t.Fatalf("low-level overwrite required profile = %q, want %q", got, ProfileLocalAuthoritative)
	}
}

func TestCapabilityOrderCanBeDeclaredBeforeRegistrations(t *testing.T) {
	isolateCapabilityRegistry(t)

	SetCapabilityOrder([]string{"second", "first"})
	SetCapabilitySupport("first", CapabilitySupport{})
	SetCapabilitySupport("second", CapabilitySupport{})

	registrations := CapabilityRegistrations()
	if got, want := len(registrations), 2; got != want {
		t.Fatalf("registrations = %d, want %d", got, want)
	}
	if got := registrations[0].Capability; got != "second" {
		t.Fatalf("registration[0] = %q, want second", got)
	}
	if got := registrations[1].Capability; got != "first" {
		t.Fatalf("registration[1] = %q, want first", got)
	}
}

func TestCapabilityRegistrationsRejectsInvalidCanonicalOrder(t *testing.T) {
	tests := []struct {
		name  string
		order []string
	}{
		{name: "incomplete", order: []string{"first"}},
		{name: "duplicate", order: []string{"first", "first"}},
		{name: "unknown", order: []string{"first", "missing"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateCapabilityRegistry(t)
			SetCapabilitySupport("first", CapabilitySupport{})
			SetCapabilitySupport("second", CapabilitySupport{})
			SetCapabilityOrder(tt.order)

			defer func() {
				got := recover()
				if got == nil {
					t.Fatal("CapabilityRegistrations did not reject invalid canonical order")
				}
				message, ok := got.(string)
				if !ok || !strings.Contains(message, "canonical capability order") {
					t.Fatalf("panic = %v, want canonical capability order diagnostic", got)
				}
			}()

			_ = CapabilityRegistrations()
		})
	}
}

func isolateCapabilityRegistry(t *testing.T) {
	t.Helper()
	originalRegistry := capabilityRegistry
	originalOrder := capabilityOrder
	originalRequestedOrder := requestedCapabilityOrder
	originalDuplicates := duplicateRegistrationKeys
	t.Cleanup(func() {
		capabilityRegistry = originalRegistry
		capabilityOrder = originalOrder
		requestedCapabilityOrder = originalRequestedOrder
		duplicateRegistrationKeys = originalDuplicates
	})

	capabilityRegistry = map[string]CapabilitySupport{}
	capabilityOrder = nil
	requestedCapabilityOrder = nil
	duplicateRegistrationKeys = nil
}
