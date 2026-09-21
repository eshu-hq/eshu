// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"io"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
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
