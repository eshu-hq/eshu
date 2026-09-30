// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildIngesterProjectorServiceWiresDeltaBaselineFence fails when the
// ingester binary builds its projector Service without the #7319 fence.
func TestBuildIngesterProjectorServiceWiresDeltaBaselineFence(t *testing.T) {
	t.Parallel()
	service, err := buildIngesterProjectorService(postgres.SQLDB{}, &noopCanonicalWriter{},
		func(string) string { return "" }, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildIngesterProjectorService() = %v", err)
	}
	if service.DeltaBaselineFence == nil {
		t.Fatal("DeltaBaselineFence = nil, want the projector queue")
	}
	if _, ok := service.DeltaBaselineFence.(postgres.ProjectorQueue); !ok {
		t.Fatalf("DeltaBaselineFence type = %T, want postgres.ProjectorQueue", service.DeltaBaselineFence)
	}
}

// TestBuildIngesterProjectorServiceWiresWriteMarker fails when the binary builds its projector Service
// without the #7389 projection write-start marker.
func TestBuildIngesterProjectorServiceWiresWriteMarker(t *testing.T) {
	t.Parallel()
	service, err := buildIngesterProjectorService(postgres.SQLDB{}, &noopCanonicalWriter{},
		func(string) string { return "" }, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildIngesterProjectorService() = %v", err)
	}
	if _, ok := service.WriteMarker.(postgres.ProjectorQueue); !ok {
		t.Fatalf("WriteMarker type = %T, want postgres.ProjectorQueue", service.WriteMarker)
	}
}
