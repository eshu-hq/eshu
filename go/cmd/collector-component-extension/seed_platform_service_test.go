// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace/noop"

	scannercomponent "github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
)

// TestBuildClaimedServiceAsksTheHostPlatformSource stubs the platform source to
// a platform no seed pin lists and requires buildClaimedService, the call the
// worker process makes at startup, to refuse a seeded satellite. It guards the
// production line (loadRuntimeConfigObserved in service.go) on every host,
// including linux/amd64, where a hardcoded pinned platform would otherwise stay
// green (#10739). It stubs a package variable, so it must not run in parallel.
func TestBuildClaimedServiceAsksTheHostPlatformSource(t *testing.T) {
	const unpinned = "plan9/mips"
	previous := seedPlatformSource
	seedPlatformSource = func() string { return unpinned }
	t.Cleanup(func() { seedPlatformSource = previous })

	home := t.TempDir()
	id := installSatelliteComponent(t, home, scannercomponent.SatelliteDocumentImportID, "")

	_, err := buildClaimedService(&fakeExecQueryer{}, satelliteWorkerEnv(home, id), noop.NewTracerProvider().Tracer("test"), nil, nil)
	if err == nil {
		t.Fatalf("buildClaimedService() with host platform %q error = nil, want refusal", unpinned)
	}
	if want := fmt.Sprintf("declares no seed image for platform %q", unpinned); !strings.Contains(err.Error(), want) {
		t.Fatalf("buildClaimedService() error = %q, want it to contain %q", err, want)
	}
}
