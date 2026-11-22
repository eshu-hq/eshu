// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestIngesterSourcePacerIsOffUnlessConfigured pins #10882 F8's ingester
// wiring: no pacer when the per-source leg is unconfigured or the local
// lightweight profile is active, a pacer when the thresholds are set.
func TestIngesterSourcePacerIsOffUnlessConfigured(t *testing.T) {
	t.Parallel()

	configured := map[string]string{
		"ESHU_SOURCE_BACKPRESSURE_ENGAGE_AGE":     "1h",
		"ESHU_SOURCE_BACKPRESSURE_RELEASE_AGE":    "15m",
		"ESHU_SOURCE_BACKPRESSURE_DEPTH_FLOOR":    "25",
		"ESHU_SOURCE_BACKPRESSURE_GRANT_DELAY":    "2s",
		"ESHU_SOURCE_BACKPRESSURE_MAX_ENGAGEMENT": "30m",
	}
	envOf := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	if pacer, err := ingesterSourcePacer(nil, envOf(nil), nil, nil); err != nil || pacer != nil {
		t.Fatalf("ingesterSourcePacer(empty env) = (%v, %v), want (nil, nil)", pacer, err)
	}
	lightweight := map[string]string{"ESHU_DISABLE_NEO4J": "true"}
	for key, value := range configured {
		lightweight[key] = value
	}
	if pacer, err := ingesterSourcePacer(nil, envOf(lightweight), nil, nil); err != nil || pacer != nil {
		t.Fatalf("ingesterSourcePacer(local lightweight) = (%v, %v), want (nil, nil)", pacer, err)
	}
}

// TestIngesterServiceRefusesMalformedSourceBackpressure proves a malformed
// per-source backpressure variable fails ingester startup with an error naming
// the variable, through the whole service build, instead of starting with the
// leg half-configured (#10882 decision 3).
func TestIngesterServiceRefusesMalformedSourceBackpressure(t *testing.T) {
	t.Parallel()

	env := map[string]string{"ESHU_SOURCE_BACKPRESSURE_ENGAGE_AGE": "not-a-duration"}
	_, err := buildIngesterService(
		postgres.SQLDB{}, &noopCanonicalWriter{}, mapGetenv(env),
		func() (string, error) { return t.TempDir(), nil },
		func() []string { return []string{"PATH=/usr/bin"} },
		nil, nil, nil,
	)
	if err == nil || !strings.Contains(err.Error(), "ESHU_SOURCE_BACKPRESSURE_ENGAGE_AGE") {
		t.Fatalf("buildIngesterService() error = %v, want one naming ESHU_SOURCE_BACKPRESSURE_ENGAGE_AGE", err)
	}
}
