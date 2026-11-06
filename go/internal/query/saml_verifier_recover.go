// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// samlVerifierMeterName scopes the lazily registered SAML verifier instrument
// to this package, the same way imageQueryMeterName does: the query package is
// not handed a *telemetry.Instruments.
const samlVerifierMeterName = "eshu/go/internal/query"

// samlVerifierPanicsMetric names the counter an operator watches for a panic
// recovered inside the crewjam SAML library. It carries no labels, so its
// cardinality is one series. initSAMLVerifierInstruments registers it under the
// same literal so the telemetry-coverage verifier can see the registration;
// TestVerifySAMLResponseRecoversCrewjamPanic reads the counter by this constant,
// so a mismatch fails it.
const samlVerifierPanicsMetric = "eshu_dp_query_saml_verifier_panics_total"

// ErrSAMLVerifierPanic is returned by CrewjamSAMLVerifier.VerifySAMLResponse
// when the crewjam/saml library panicked while validating a response. The ACS
// handler treats it like any other verifier error: a 401 and an
// assertion_invalid governance-audit event.
var ErrSAMLVerifierPanic = errors.New("saml verifier: recovered panic in crewjam/saml")

var (
	samlVerifierInstrumentsOnce sync.Once
	samlVerifierPanics          metric.Int64Counter
)

// initSAMLVerifierInstruments registers the panic counter once. A registration
// error leaves the counter nil and recording becomes a no-op, so a telemetry
// fault never changes a login outcome. The meter is fetched from the current
// global provider inside the once so a test that installs its own provider
// first still observes the counter (see initImageQueryInstruments).
func initSAMLVerifierInstruments() {
	samlVerifierInstrumentsOnce.Do(func() {
		meter := otel.Meter(samlVerifierMeterName)
		var err error
		samlVerifierPanics, err = meter.Int64Counter(
			"eshu_dp_query_saml_verifier_panics_total",
			metric.WithDescription("Panics recovered inside the crewjam/saml library while verifying a SAML response"),
		)
		if err != nil {
			samlVerifierPanics = nil
		}
	})
}

// VerifySAMLResponse validates a base64 POST binding response and returns the
// assertion material Eshu needs for replay, claim mapping, and session creation.
//
// crewjam/saml has panicked on signed but incomplete assertions (GO-2026-6909).
// The Eshu fork fixes the known cases; this recover is the backstop for any
// dereference the audit missed. A panic fails the login closed: the caller gets
// ErrSAMLVerifierPanic, the stack is logged, and the panic counter increments.
// net/http would also recover the panic, but only by dropping the connection
// with no audit event.
func (v CrewjamSAMLVerifier) VerifySAMLResponse(
	ctx context.Context,
	provider SAMLProviderConfig,
	samlResponse string,
	possibleRequestIDHashes []string,
) (result SAMLAssertion, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = SAMLAssertion{}
			err = recordSAMLVerifierPanic(ctx, provider.ProviderConfigID, recovered, debug.Stack())
		}
	}()
	return v.verifySAMLResponse(ctx, provider, samlResponse, possibleRequestIDHashes)
}

// recordSAMLVerifierPanic logs the recovered panic with its stack, increments
// the panic counter, and returns the error the verifier reports.
func recordSAMLVerifierPanic(ctx context.Context, providerConfigID string, recovered any, stack []byte) error {
	initSAMLVerifierInstruments()
	if samlVerifierPanics != nil {
		samlVerifierPanics.Add(ctx, 1)
	}
	slog.ErrorContext(ctx, "saml verifier: recovered panic in crewjam/saml; login denied",
		slog.String("provider_config_id", providerConfigID),
		slog.Any("panic", recovered),
		slog.String("stack", string(stack)),
		slog.String("metric", samlVerifierPanicsMetric),
	)
	return ErrSAMLVerifierPanic
}
