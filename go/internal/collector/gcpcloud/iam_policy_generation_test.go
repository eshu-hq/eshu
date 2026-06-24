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

func testIAMPolicyResourceObservation() ResourceObservation {
	return ResourceObservation{
		Name:       "//storage.googleapis.com/projects/_/buckets/iam-bucket",
		AssetType:  "storage.googleapis.com/Bucket",
		Location:   "us-central1",
		Ancestors:  []string{"projects/123456789", "organizations/9988776655"},
		UpdateTime: time.Date(2026, 6, 9, 12, 1, 0, 0, time.UTC),
		IAMPolicyBindings: []IAMPolicyBindingObservation{
			{
				Role:                      "roles/storage.objectViewer",
				Members:                   []string{"allUsers", "serviceAccount:workload-reader"},
				ConditionPresent:          true,
				ConditionFingerprintInput: `{"expression":"request.time < timestamp('2026-12-31T00:00:00Z')","title":"expires"}`,
				Etag:                      "etag-iam-1",
			},
			{
				Role:    "roles/storage.admin",
				Members: []string{"group:platform-operators"},
				Etag:    "etag-iam-1",
			},
			{
				Role:    "roles/empty",
				Members: nil,
				Etag:    "etag-iam-1",
			},
		},
	}
}

func TestGenerationBuildEmitsIAMPolicyObservationsForBindings(t *testing.T) {
	key := testRedactionKey(t)
	gen := NewGeneration(testGenerationBoundary(), key)
	gen.ObserveReadTime(time.Date(2026, 6, 9, 12, 5, 0, 0, time.UTC))

	if err := gen.AddPage([]ResourceObservation{testIAMPolicyResourceObservation()}); err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := countKind(envelopes, facts.GCPCloudResourceFactKind); got != 1 {
		t.Fatalf("resource fact count = %d, want 1", got)
	}
	if got := countKind(envelopes, facts.GCPIAMPolicyObservationFactKind); got != 2 {
		t.Fatalf("iam policy fact count = %d, want 2", got)
	}

	var viewer facts.Envelope
	for _, env := range envelopes {
		payload := fmt.Sprintf("%#v", env.Payload)
		for _, forbidden := range []string{"serviceAccount:workload-reader", "group:platform-operators", "etag-iam-1", "request.time"} {
			if strings.Contains(payload, forbidden) {
				t.Fatalf("raw IAM value leaked in %s payload: %s", env.FactKind, payload)
			}
		}
		if env.FactKind == facts.GCPIAMPolicyObservationFactKind &&
			env.Payload["role"] == "roles/storage.objectViewer" {
			viewer = env
		}
	}
	if viewer.FactKind == "" {
		t.Fatal("viewer IAM observation missing")
	}
	if viewer.Payload["condition_present"] != true {
		t.Fatalf("condition_present = %#v, want true", viewer.Payload["condition_present"])
	}
	if viewer.Payload["read_time"] == nil {
		t.Fatal("read_time missing from IAM observation")
	}
	members, ok := viewer.Payload["members"].([]map[string]string)
	if !ok || len(members) != 2 {
		t.Fatalf("members = %#v, want two fingerprinted members", viewer.Payload["members"])
	}
}

func TestGenerationBuildSkipsIAMPolicyObservationsWithoutRedactionKey(t *testing.T) {
	gen := NewGeneration(testGenerationBoundary(), redact.Key{})
	if err := gen.AddPage([]ResourceObservation{testIAMPolicyResourceObservation()}); err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	envelopes, err := gen.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := countKind(envelopes, facts.GCPCloudResourceFactKind); got != 1 {
		t.Fatalf("resource fact count = %d, want 1", got)
	}
	if got := countKind(envelopes, facts.GCPIAMPolicyObservationFactKind); got != 0 {
		t.Fatalf("iam policy fact count = %d, want 0", got)
	}
}
