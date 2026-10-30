// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPrometheusExposesGoRuntimeMemoryMetrics is the #9567 regression: the
// QA reducer's /metrics carried no Go heap or process memory series and no
// memory-limit gauge, so an operator could not see the heap burst against the
// limit that preceded each OOM kill. Every runtime builds its /metrics through
// NewProviders, so NewProviders alone, with no per-runtime call, must expose
// eshu_dp_gomemlimit_bytes and the Go collector's heap and GC series (and
// process resident memory where the platform supports it).
func TestPrometheusExposesGoRuntimeMemoryMetrics(t *testing.T) {
	_ = os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	b, err := NewBootstrap("runtime-memory-test")
	require.NoError(t, err)
	ctx := context.Background()
	providers, err := NewProviders(ctx, b)
	require.NoError(t, err)
	defer func() { _ = providers.Shutdown(ctx) }()

	rec := httptest.NewRecorder()
	providers.PrometheusHandler.ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	body := rec.Body.String()

	want := []string{
		"eshu_dp_gomemlimit_bytes",
		"go_memstats_heap_alloc_bytes",
		"go_memstats_heap_inuse_bytes",
		"go_memstats_heap_released_bytes",
		"go_memstats_sys_bytes",
		"go_gc_duration_seconds",
		"go_goroutines",
	}
	if runtime.GOOS == "linux" {
		want = append(want, "process_resident_memory_bytes")
	}
	for _, name := range want {
		if !strings.Contains(body, "\n"+name) {
			t.Errorf("/metrics is missing %s", name)
		}
	}
}
