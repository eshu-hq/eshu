// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reduceradmission"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type lightweightCanonicalWriter struct{}

func (lightweightCanonicalWriter) Write(context.Context, canonical.CanonicalMaterialization) error {
	return nil
}

type lightweightReducerIntentWriter struct{}

func (lightweightReducerIntentWriter) Enqueue(_ context.Context, intents []runtime.ReducerIntent) (runtime.IntentResult, error) {
	return runtime.IntentResult{Count: len(intents)}, nil
}

type noopCloser struct{}

func (noopCloser) Close() error {
	return nil
}

func ingesterLocalLightweight(getenv func(string) string) bool {
	if strings.EqualFold(strings.TrimSpace(getenv("ESHU_DISABLE_NEO4J")), "true") {
		return true
	}
	return strings.TrimSpace(getenv("ESHU_QUERY_PROFILE")) == "local_lightweight"
}

func maybeLocalLightweightCanonicalWriter(getenv func(string) string) (runtime.CanonicalWriter, io.Closer, bool) {
	if !ingesterLocalLightweight(getenv) {
		return nil, nil, false
	}
	return lightweightCanonicalWriter{}, noopCloser{}, true
}

func reducerIntentWriterForProfile(getenv func(string) string, fallback runtime.ReducerIntentWriter) runtime.ReducerIntentWriter {
	if ingesterLocalLightweight(getenv) {
		return lightweightReducerIntentWriter{}
	}
	return fallback
}

// ingesterAdmissionIntentWriter builds the ingester's reducer intent
// writer: the deployment profile's inner queue writer wrapped with the
// shared reducer admission gate (#10882 decision 3). The local
// lightweight profile has no real reducer queue behind it, so it keeps
// the profile writer unwrapped instead of gating against a database that
// is not there.
func ingesterAdmissionIntentWriter(
	database db.ExecQueryer,
	getenv func(string) string,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (runtime.ReducerIntentWriter, error) {
	inner := reducerIntentWriterForProfile(getenv, postgres.NewReducerQueue(database, "ingester", time.Minute))
	if ingesterLocalLightweight(getenv) {
		return inner, nil
	}
	return reduceradmission.AdmissionIntentWriter(database, inner, getenv, instruments, logger)
}
