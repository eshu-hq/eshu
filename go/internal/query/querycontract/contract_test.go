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

func TestMinTruthLevelReturnsLowerRank(t *testing.T) {
	tests := []struct {
		name string
		a    TruthLevel
		b    TruthLevel
		want TruthLevel
	}{
		{name: "exact exact", a: TruthLevelExact, b: TruthLevelExact, want: TruthLevelExact},
		{name: "exact derived", a: TruthLevelExact, b: TruthLevelDerived, want: TruthLevelDerived},
		{name: "derived exact", a: TruthLevelDerived, b: TruthLevelExact, want: TruthLevelDerived},
		{name: "derived fallback", a: TruthLevelDerived, b: TruthLevelFallback, want: TruthLevelFallback},
		{name: "fallback derived", a: TruthLevelFallback, b: TruthLevelDerived, want: TruthLevelFallback},
		{name: "unknown is conservative", a: TruthLevelExact, b: TruthLevel("unknown"), want: TruthLevel("unknown")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MinTruthLevel(tt.a, tt.b); got != tt.want {
				t.Fatalf("MinTruthLevel(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
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

// TestNoBackendReadBasisIsFallbackAndSurvivesNormalization pins the two
// querycontract-side contracts #6544 introduced with TruthBasisNoBackendRead.
//
// basisLevel must answer TruthLevelFallback for it, and must do so from its own
// case rather than the unrecognized-value default arm: a page produced without
// reading anything holds no evidence, so it is neither exact nor derived FROM
// content. normalizeTruthBasis must leave it alone on every profile -- its one
// rewrite (authoritative_graph -> hybrid under local_lightweight) exists because
// that profile cannot serve authoritative graph truth, and silently rewriting a
// no-read page's basis would put back exactly the false claim the member was
// added to remove.
func TestNoBackendReadBasisIsFallbackAndSurvivesNormalization(t *testing.T) {
	isolateCapabilityRegistry(t)

	exact := TruthLevelExact
	RegisterCapabilities(CapabilityRegistration{
		Capability: "test.no_backend_read",
		Support: CapabilitySupport{
			LocalLightweightMax:   &exact,
			LocalAuthoritativeMax: &exact,
			LocalFullStackMax:     &exact,
			ProductionMax:         &exact,
			RequiredProfile:       ProfileLocalLightweight,
		},
	})

	for _, profile := range []QueryProfile{
		ProfileLocalLightweight,
		ProfileLocalAuthoritative,
		ProfileLocalFullStack,
		ProfileProduction,
	} {
		truth := BuildTruthEnvelope(profile, "test.no_backend_read", TruthBasisNoBackendRead, "test")
		// The capability ceiling above is exact on every profile, so the
		// fallback below is basisLevel's answer and not a ceiling clamp.
		if got, want := truth.Level, TruthLevelFallback; got != want {
			t.Fatalf("profile %q: truth level = %q, want %q; a page produced without a read is not exact or derived", profile, got, want)
		}
		if got, want := truth.Basis, TruthBasisNoBackendRead; got != want {
			t.Fatalf("profile %q: truth basis = %q, want %q; normalizeTruthBasis must not rewrite a no-read basis", profile, got, want)
		}
	}

	if got, want := string(TruthBasisNoBackendRead), "no_backend_read"; got != want {
		t.Fatalf("TruthBasisNoBackendRead = %q, want %q; the wire spelling is a published contract "+
			"(docs/public/reference/truth-label-protocol.md)", got, want)
	}
}
