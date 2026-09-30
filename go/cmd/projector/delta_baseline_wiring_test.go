// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildProjectorServiceWiresDeltaBaselineFence fails when the projector
// binary builds its Service without the #7319 delta-baseline fence.
func TestBuildProjectorServiceWiresDeltaBaselineFence(t *testing.T) {
	t.Parallel()
	service, err := buildProjectorService(postgres.SQLDB{}, &noopCanonicalWriter{},
		func(string) string { return "" }, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildProjectorService() = %v", err)
	}
	if service.DeltaBaselineFence == nil {
		t.Fatal("DeltaBaselineFence = nil, want the projector queue")
	}
	if _, ok := service.DeltaBaselineFence.(postgres.ProjectorQueue); !ok {
		t.Fatalf("DeltaBaselineFence type = %T, want postgres.ProjectorQueue", service.DeltaBaselineFence)
	}
}

// TestBuildProjectorServiceWiresWriteMarker fails when the binary builds its projector Service
// without the #7389 projection write-start marker.
func TestBuildProjectorServiceWiresWriteMarker(t *testing.T) {
	t.Parallel()
	service, err := buildProjectorService(postgres.SQLDB{}, &noopCanonicalWriter{},
		func(string) string { return "" }, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildProjectorService() = %v", err)
	}
	if _, ok := service.WriteMarker.(postgres.ProjectorQueue); !ok {
		t.Fatalf("WriteMarker type = %T, want postgres.ProjectorQueue", service.WriteMarker)
	}
}
