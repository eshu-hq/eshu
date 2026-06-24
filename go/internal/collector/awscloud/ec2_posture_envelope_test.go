// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestNewEC2InstancePostureEnvelopeCarriesDerivedPosture(t *testing.T) {
	t.Parallel()

	boundary := testBoundary(time.Date(2026, 5, 31, 18, 30, 0, 0, time.UTC))
	boundary.Region = "us-east-1"
	boundary.ServiceKind = ServiceEC2
	imdsv2 := true
	hopLimit := int32(1)
	userData := true
	volumeEncrypted := true
	observation := EC2InstancePostureObservation{
		Boundary:                boundary,
		ARN:                     "arn:aws:ec2:us-east-1:123456789012:instance/i-1234567890abcdef0",
		InstanceID:              "i-1234567890abcdef0",
		State:                   "running",
		IMDSv2Required:          &imdsv2,
		HTTPEndpoint:            "enabled",
		HTTPPutResponseHopLimit: &hopLimit,
		UserDataPresent:         &userData,
		DetailedMonitoring:      true,
		EBSOptimized:            true,
		PublicIPAssociated:      true,
		PublicIPAddress:         "203.0.113.10",
		InstanceProfileARN:      "arn:aws:iam::123456789012:instance-profile/app",
		Tenancy:                 "default",
		NitroEnclaveEnabled:     true,
		BlockDevices: []EC2BlockDevicePosture{{
			DeviceName:          "/dev/xvda",
			VolumeID:            "vol-0abc",
			DeleteOnTermination: true,
			Status:              "attached",
			Encrypted:           &volumeEncrypted,
		}},
	}

	envelope, err := NewEC2InstancePostureEnvelope(observation)
	if err != nil {
		t.Fatalf("NewEC2InstancePostureEnvelope() error = %v, want nil", err)
	}

	if envelope.FactKind != facts.EC2InstancePostureFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.EC2InstancePostureFactKind)
	}
	if envelope.SchemaVersion != facts.EC2InstancePostureSchemaVersionV1 {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.EC2InstancePostureSchemaVersionV1)
	}
	if envelope.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, CollectorKind)
	}
	if envelope.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want %q", envelope.SourceConfidence, facts.SourceConfidenceReported)
	}

	payload := envelope.Payload
	wantValues := map[string]any{
		"resource_type":               ResourceTypeEC2Instance,
		"instance_id":                 "i-1234567890abcdef0",
		"service_kind":                ServiceEC2,
		"state":                       "running",
		"imds_v2_required":            true,
		"imds_http_endpoint":          "enabled",
		"imds_http_put_hop_limit":     int32(1),
		"user_data_present":           true,
		"detailed_monitoring_enabled": true,
		"ebs_optimized":               true,
		"public_ip_associated":        true,
		"public_ip_address":           "203.0.113.10",
		"instance_profile_arn":        "arn:aws:iam::123456789012:instance-profile/app",
		"tenancy":                     "default",
		"nitro_enclave_enabled":       true,
	}
	for key, want := range wantValues {
		if got := payload[key]; got != want {
			t.Fatalf("payload[%q] = %#v, want %#v", key, got, want)
		}
	}

	devices, ok := payload["block_devices"].([]map[string]any)
	if !ok || len(devices) != 1 {
		t.Fatalf("block_devices = %#v, want one entry", payload["block_devices"])
	}
	if got := devices[0]["volume_id"]; got != "vol-0abc" {
		t.Fatalf("block_devices[0].volume_id = %#v, want vol-0abc", got)
	}
	if got := devices[0]["encrypted"]; got != true {
		t.Fatalf("block_devices[0].encrypted = %#v, want true", got)
	}

	anchors, ok := payload["correlation_anchors"].([]string)
	if !ok || len(anchors) == 0 {
		t.Fatalf("correlation_anchors = %#v, want non-empty", payload["correlation_anchors"])
	}

	for _, forbidden := range []string{
		"user_data",
		"user_data_content",
		"console_output",
		"environment",
		"secret",
		"password",
	} {
		if _, exists := payload[forbidden]; exists {
			t.Fatalf("%s persisted on posture fact; EC2 posture must stay metadata-only", forbidden)
		}
	}
}

func TestNewEC2InstancePostureEnvelopeKeepsUnknownSettingsNil(t *testing.T) {
	t.Parallel()

	boundary := testBoundary(time.Date(2026, 5, 31, 18, 30, 0, 0, time.UTC))
	boundary.Region = "us-east-1"
	boundary.ServiceKind = ServiceEC2
	envelope, err := NewEC2InstancePostureEnvelope(EC2InstancePostureObservation{
		Boundary:   boundary,
		InstanceID: "i-unknown",
		State:      "running",
	})
	if err != nil {
		t.Fatalf("NewEC2InstancePostureEnvelope() error = %v, want nil", err)
	}
	for _, key := range []string{"imds_v2_required", "imds_http_put_hop_limit", "user_data_present"} {
		if got := envelope.Payload[key]; got != nil {
			t.Fatalf("payload[%q] = %#v, want nil for an unreported setting", key, got)
		}
	}
	if got, _ := envelope.Payload["block_devices"].([]map[string]any); len(got) != 0 {
		t.Fatalf("block_devices = %#v, want empty for no devices", got)
	}
}

func TestNewEC2InstancePostureEnvelopeRequiresIdentity(t *testing.T) {
	t.Parallel()

	boundary := testBoundary(time.Date(2026, 5, 31, 18, 30, 0, 0, time.UTC))
	boundary.ServiceKind = ServiceEC2
	if _, err := NewEC2InstancePostureEnvelope(EC2InstancePostureObservation{Boundary: boundary}); err == nil {
		t.Fatalf("NewEC2InstancePostureEnvelope() error = nil, want missing-identity error")
	}
}

func TestNewEC2InstancePostureEnvelopeStableKeyVariesByInstance(t *testing.T) {
	t.Parallel()

	boundary := testBoundary(time.Date(2026, 5, 31, 18, 30, 0, 0, time.UTC))
	boundary.ServiceKind = ServiceEC2
	first, err := NewEC2InstancePostureEnvelope(EC2InstancePostureObservation{Boundary: boundary, InstanceID: "i-aaa"})
	if err != nil {
		t.Fatalf("first posture error = %v", err)
	}
	second, err := NewEC2InstancePostureEnvelope(EC2InstancePostureObservation{Boundary: boundary, InstanceID: "i-bbb"})
	if err != nil {
		t.Fatalf("second posture error = %v", err)
	}
	if first.StableFactKey == second.StableFactKey {
		t.Fatalf("distinct instances share StableFactKey %q", first.StableFactKey)
	}
}
