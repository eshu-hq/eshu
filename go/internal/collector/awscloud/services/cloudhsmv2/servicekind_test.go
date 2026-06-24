// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudhsmv2_test

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudhsmv2"
)

// TestScannerCanonicalizesPaddedServiceKind proves a whitespace-padded
// service_kind is written back as the canonical value on every emitted fact.
// The Scan switch trims only for the comparison, so without the write-back the
// padded string leaks into each fact's service_kind and breaks graph
// joins/filters that key on the canonical "cloudhsmv2".
func TestScannerCanonicalizesPaddedServiceKind(t *testing.T) {
	boundary := awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         "  " + awscloud.ServiceCloudHSMV2 + "  ",
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:cloudhsmv2:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
	scanner := cloudhsmv2.Scanner{Client: paddedFakeClient{}}

	envelopes, err := scanner.Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, envelope := range envelopes {
		if got, want := envelope.Payload["service_kind"], awscloud.ServiceCloudHSMV2; got != want {
			t.Fatalf("envelope service_kind = %#v, want %q (padded service_kind must be canonicalized)", got, want)
		}
	}
}

type paddedFakeClient struct{}

func (paddedFakeClient) Snapshot(context.Context) (cloudhsmv2.Snapshot, error) {
	return cloudhsmv2.Snapshot{
		Clusters: []cloudhsmv2.Cluster{{
			ID:    "cluster-padded123456",
			VPCID: "vpc-0123456789abcdef0",
		}},
		Backups: []cloudhsmv2.Backup{{
			ID:        "backup-padded1234567",
			ClusterID: "cluster-padded123456",
		}},
	}, nil
}
