// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// fakePresenceLookup reports either all queried uids missing or all present.
type fakePresenceLookup struct {
	allMissing bool
	err        error
	calls      int
}

func (l *fakePresenceLookup) MissingUIDs(_ context.Context, _ GraphProjectionKeyspace, uids []string) ([]string, error) {
	l.calls++
	if l.err != nil {
		return nil, l.err
	}
	if l.allMissing {
		return uids, nil
	}
	return nil, nil
}

func presenceProjectionHandler(loader fakeFactLoader, writer *recordingGraphWriter, lookup EndpointPresenceLookup) SecretsIAMGraphProjectionHandler {
	return SecretsIAMGraphProjectionHandler{FactLoader: loader, Writer: writer, PresenceLookup: lookup}
}

func TestGraphProjectionGateReEnqueuesWhenEndpointNotReady(t *testing.T) {
	t.Parallel()

	loader := fakeFactLoader{envelopes: []facts.Envelope{exactChainFact(fullExactChainPayload())}}
	writer := &recordingGraphWriter{}
	h := presenceProjectionHandler(loader, writer, &fakePresenceLookup{allMissing: true})

	_, err := h.Handle(context.Background(), graphProjectionIntent())
	if err == nil {
		t.Fatal("Handle error = nil, want retryable not-ready error")
	}
	var notReady secretsIAMEndpointsNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("error = %T (%v), want secretsIAMEndpointsNotReadyError", err, err)
	}
	if !notReady.Retryable() || notReady.FailureClass() != "secrets_iam_endpoint_not_ready" {
		t.Fatalf("not-ready error contract wrong: retryable=%v class=%q", notReady.Retryable(), notReady.FailureClass())
	}
	// Gate runs BEFORE retract/write: prior graph state is untouched.
	if len(writer.retracts) != 0 {
		t.Fatalf("retract ran while endpoints not ready: %v", writer.retracts)
	}
	if len(writer.serviceAccountNodes) != 0 || len(writer.usesSAEdges) != 0 {
		t.Fatal("projection wrote rows while endpoints not ready")
	}
}

func TestGraphProjectionGateWritesWhenEndpointsReady(t *testing.T) {
	t.Parallel()

	loader := fakeFactLoader{envelopes: []facts.Envelope{exactChainFact(fullExactChainPayload())}}
	writer := &recordingGraphWriter{}
	lookup := &fakePresenceLookup{allMissing: false}
	h := presenceProjectionHandler(loader, writer, lookup)

	res, err := h.Handle(context.Background(), graphProjectionIntent())
	if err != nil {
		t.Fatalf("Handle error = %v, want nil when endpoints ready", err)
	}
	if lookup.calls == 0 {
		t.Fatal("presence lookup never queried")
	}
	if len(writer.serviceAccountNodes) == 0 || len(writer.usesSAEdges) == 0 {
		t.Fatal("ready endpoints did not produce node/edge writes")
	}
	if res.CanonicalWrites == 0 {
		t.Fatal("CanonicalWrites = 0 for a ready exact chain")
	}
}

func TestGraphProjectionGateDisabledWhenLookupNil(t *testing.T) {
	t.Parallel()

	loader := fakeFactLoader{envelopes: []facts.Envelope{exactChainFact(fullExactChainPayload())}}
	writer := &recordingGraphWriter{}
	h := SecretsIAMGraphProjectionHandler{FactLoader: loader, Writer: writer} // PresenceLookup nil

	if _, err := h.Handle(context.Background(), graphProjectionIntent()); err != nil {
		t.Fatalf("Handle error = %v, want nil (gating disabled)", err)
	}
	if len(writer.serviceAccountNodes) == 0 {
		t.Fatal("nil lookup changed projection behavior")
	}
}

func TestGraphProjectionGateSurfacesLookupError(t *testing.T) {
	t.Parallel()

	loader := fakeFactLoader{envelopes: []facts.Envelope{exactChainFact(fullExactChainPayload())}}
	writer := &recordingGraphWriter{}
	h := presenceProjectionHandler(loader, writer, &fakePresenceLookup{err: errors.New("db down")})

	if _, err := h.Handle(context.Background(), graphProjectionIntent()); err == nil {
		t.Fatal("Handle error = nil, want lookup error surfaced")
	}
	if len(writer.serviceAccountNodes) != 0 {
		t.Fatal("projection wrote rows despite a presence lookup error")
	}
}
