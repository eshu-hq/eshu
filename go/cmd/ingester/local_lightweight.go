package main

import (
	"context"
	"io"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

type lightweightCanonicalWriter struct{}

func (lightweightCanonicalWriter) Write(context.Context, projector.CanonicalMaterialization) error {
	return nil
}

type lightweightReducerIntentWriter struct{}

func (lightweightReducerIntentWriter) Enqueue(_ context.Context, intents []projector.ReducerIntent) (projector.IntentResult, error) {
	return projector.IntentResult{Count: len(intents)}, nil
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

func maybeLocalLightweightCanonicalWriter(getenv func(string) string) (projector.CanonicalWriter, io.Closer, bool) {
	if !ingesterLocalLightweight(getenv) {
		return nil, nil, false
	}
	return lightweightCanonicalWriter{}, noopCloser{}, true
}

func reducerIntentWriterForProfile(getenv func(string) string, fallback projector.ReducerIntentWriter) projector.ReducerIntentWriter {
	if ingesterLocalLightweight(getenv) {
		return lightweightReducerIntentWriter{}
	}
	return fallback
}
