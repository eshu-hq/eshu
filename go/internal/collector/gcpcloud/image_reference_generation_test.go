// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testImageReferenceResourceObservation() ResourceObservation {
	return ResourceObservation{
		Name:       "//run.googleapis.com/projects/my-project/locations/us-central1/services/api-service",
		AssetType:  "run.googleapis.com/Service",
		Location:   "us-central1",
		Ancestors:  []string{"projects/123456789", "organizations/9988776655"},
		UpdateTime: time.Date(2026, 6, 9, 12, 4, 0, 0, time.UTC),
		ImageReferences: []ImageReferenceObservation{
			{
				ImageReference: "us-docker.pkg.dev/my-project/team/api@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				ImageDigest:    "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				ContainerName:  "api",
			},
			{
				ImageReference: "us-docker.pkg.dev/my-project/team/worker:2026-06-09",
				ContainerName:  "worker",
			},
		},
	}
}

func TestGenerationBuildEmitsImageReferenceObservationsForRuntimeContainers(t *testing.T) {
	key := testRedactionKey(t)
	gen := NewGeneration(testGenerationBoundary(), key)
	gen.ObserveReadTime(time.Date(2026, 6, 9, 12, 15, 0, 0, time.UTC))

	if err := gen.AddPage([]ResourceObservation{testImageReferenceResourceObservation()}); err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := countKind(envelopes, facts.GCPCloudResourceFactKind); got != 1 {
		t.Fatalf("resource fact count = %d, want 1", got)
	}
	if got := countKind(envelopes, facts.GCPImageReferenceFactKind); got != 2 {
		t.Fatalf("image reference fact count = %d, want 2", got)
	}

	image := firstFactKind(t, envelopes, facts.GCPImageReferenceFactKind)
	if image.Payload["read_time"] == nil {
		t.Fatal("read_time missing from image reference observation")
	}
	if image.Payload["container_name_fingerprint"] == "api" || image.Payload["container_name_fingerprint"] == "worker" {
		t.Fatalf("container_name_fingerprint leaked raw name: %#v", image.Payload["container_name_fingerprint"])
	}
	for _, env := range envelopes {
		payload := fmt.Sprintf("%#v", env.Payload)
		sourceRef := fmt.Sprintf("%#v", env.SourceRef)
		for _, forbidden := range []string{"ContainerName:api", "ContainerName:worker"} {
			if strings.Contains(payload, forbidden) {
				t.Fatalf("raw container field leaked in %s payload: %s", env.FactKind, payload)
			}
			if strings.Contains(sourceRef, forbidden) {
				t.Fatalf("raw container field leaked in %s source ref: %s", env.FactKind, sourceRef)
			}
		}
	}
}

func TestGenerationBuildSkipsImageReferenceObservationsWithoutRedactionKey(t *testing.T) {
	gen := NewGeneration(testGenerationBoundary(), redact.Key{})
	if err := gen.AddPage([]ResourceObservation{testImageReferenceResourceObservation()}); err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := countKind(envelopes, facts.GCPCloudResourceFactKind); got != 1 {
		t.Fatalf("resource fact count = %d, want 1", got)
	}
	if got := countKind(envelopes, facts.GCPImageReferenceFactKind); got != 0 {
		t.Fatalf("image reference fact count = %d, want 0", got)
	}
}
