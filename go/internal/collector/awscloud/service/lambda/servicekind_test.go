// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lambda

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestScannerCanonicalizesPaddedServiceKind proves a whitespace-padded
// service_kind is written back as the canonical value on every emitted fact.
// The Scan switch trims only for the comparison, so without the write-back the
// padded string leaks into each fact's service_kind and breaks graph
// joins/filters that key on the canonical "lambda".
func TestScannerCanonicalizesPaddedServiceKind(t *testing.T) {
	key, err := redact.NewKey([]byte("lambda-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	boundary := testBoundary()
	boundary.ServiceKind = "  " + awscloud.ServiceLambda + "  "
	functionARN := "arn:aws:lambda:us-east-1:123456789012:function:api"
	imageURI := "123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api:prod"
	client := fakeClient{
		functions: []Function{{
			ARN:              functionARN,
			Name:             "api",
			Runtime:          "nodejs20.x",
			RoleARN:          "arn:aws:iam::123456789012:role/api-lambda",
			Handler:          "index.handler",
			PackageType:      "Image",
			ImageURI:         imageURI,
			ResolvedImageURI: "123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api@sha256:abc123",
			Version:          "$LATEST",
			State:            "Active",
			LastModified:     time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
			Environment: map[string]string{
				"DATABASE_URL": "postgres://user:password@example.internal/app",
				"LOG_LEVEL":    "public-looking-but-still-runtime-config",
			},
			VPCConfig: VPCConfig{
				VPCID:            "vpc-123",
				SubnetIDs:        []string{"subnet-a", "subnet-b"},
				SecurityGroupIDs: []string{"sg-123"},
				IPv6AllowedForDS: true,
			},
			Tags: map[string]string{"environment": "prod"},
		}},
		aliases: map[string][]Alias{
			functionARN: {{
				ARN:             functionARN + ":prod",
				Name:            "prod",
				FunctionARN:     functionARN,
				FunctionVersion: "12",
				RoutingWeights:  map[string]float64{"13": 0.1},
			}},
		},
		eventSourceMappings: map[string][]EventSourceMapping{
			functionARN: {{
				ARN:            "arn:aws:lambda:us-east-1:123456789012:event-source-mapping:11111111-2222-3333-4444-555555555555",
				UUID:           "11111111-2222-3333-4444-555555555555",
				FunctionARN:    functionARN,
				EventSourceARN: "arn:aws:sqs:us-east-1:123456789012:api-events",
				State:          "Enabled",
				BatchSize:      10,
			}},
		},
	}

	envelopes, err := (Scanner{Client: client, RedactionKey: key}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, envelope := range envelopes {
		if got, want := envelope.Payload["service_kind"], awscloud.ServiceLambda; got != want {
			t.Fatalf("envelope service_kind = %#v, want %q (padded service_kind must be canonicalized)", got, want)
		}
	}
}
