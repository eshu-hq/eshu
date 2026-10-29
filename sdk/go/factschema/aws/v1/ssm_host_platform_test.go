// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// TestSSMHostPlatformJSONSpelling pins the exact v1.0.0 wire keys for
// eshu-hq/eshu#9556: the EC2 host tuple, the platform fields when present,
// the source type with its provenance, and the observation time. The
// inventory capture time is present only for inventory-sourced platform
// evidence, never invented for DescribeInstanceInformation observations.
func TestSSMHostPlatformJSONSpelling(t *testing.T) {
	t.Parallel()

	platformType, platformName, platformVersion := "Linux", "AmazonLinux", "2023.6.20241010"
	collector := "collector-1"
	platform := SSMHostPlatform{
		Partition:           "aws",
		AccountID:           "055929692761",
		Region:              "us-east-1",
		InstanceID:          "i-0e18f7b9871b85f57",
		ResourceType:        "AWS::EC2::Instance",
		PlatformType:        &platformType,
		PlatformName:        &platformName,
		PlatformVersion:     &platformVersion,
		SourceType:          SSMHostPlatformSourceInventory,
		CaptureTime:         ptr("2026-10-29T22:31:19Z"),
		ObservationTime:     "2026-10-29T22:33:02Z",
		CollectorInstanceID: &collector,
	}

	raw, err := json.Marshal(platform)
	if err != nil {
		t.Fatalf("json.Marshal(SSMHostPlatform) error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(marshaled) error = %v", err)
	}
	var got []string
	for key := range decoded {
		got = append(got, key)
	}
	sort.Strings(got)
	want := []string{
		"account_id", "capture_time", "collector_instance_id",
		"instance_id", "observation_time", "partition", "platform_name",
		"platform_type", "platform_version", "region", "resource_type",
		"source_type",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("wire keys = %v, want %v", got, want)
	}
}

// TestSSMHostPlatformDescribeInstanceInformationOmitsCaptureTime proves the
// separate DescribeInstanceInformation evidence rule from the accepted
// #9302 record: a platform observation from DescribeInstanceInformation
// carries its observation time with that provenance and must not label any
// invented inventory capture time.
func TestSSMHostPlatformDescribeInstanceInformationOmitsCaptureTime(t *testing.T) {
	t.Parallel()

	platformType := "Linux"
	platform := SSMHostPlatform{
		Partition:       "aws",
		AccountID:       "055929692761",
		Region:          "us-east-1",
		InstanceID:      "i-0e18f7b9871b85f57",
		ResourceType:    "AWS::EC2::Instance",
		PlatformType:    &platformType,
		SourceType:      SSMHostPlatformSourceDescribeInstanceInformation,
		ObservationTime: "2026-10-29T22:33:02Z",
	}
	raw, err := json.Marshal(platform)
	if err != nil {
		t.Fatalf("json.Marshal(SSMHostPlatform) error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(marshaled) error = %v", err)
	}
	if _, ok := decoded["capture_time"]; ok {
		t.Fatalf("DescribeInstanceInformation observation carries capture_time %v", decoded["capture_time"])
	}
	if decoded["observation_time"] != "2026-10-29T22:33:02Z" {
		t.Fatalf("observation_time = %v, want the DescribeInstanceInformation read time", decoded["observation_time"])
	}
}

// TestSSMHostPlatformStableKeyComposition proves the stable key carries the
// host tuple plus the source type, and that the two provenances key
// distinctly: inventory and DescribeInstanceInformation evidence for one host
// never collide.
func TestSSMHostPlatformStableKeyComposition(t *testing.T) {
	t.Parallel()

	base := SSMHostPlatform{
		Partition: "aws", AccountID: "055929692761", Region: "us-east-1",
		InstanceID:      "i-0e18f7b9871b85f57",
		ResourceType:    "AWS::EC2::Instance",
		SourceType:      SSMHostPlatformSourceInventory,
		CaptureTime:     ptr("2026-10-29T22:31:19Z"),
		ObservationTime: "2026-10-29T22:33:02Z",
	}
	key := SSMHostPlatformStableKey(base)
	for _, want := range []string{
		"aws_ssm_host_platform", "aws", "055929692761", "us-east-1",
		"i-0e18f7b9871b85f57", SSMHostPlatformSourceInventory,
	} {
		if !strings.Contains(key, want) {
			t.Fatalf("stable key %q misses %q", key, want)
		}
	}

	viaDII := base
	viaDII.SourceType = SSMHostPlatformSourceDescribeInstanceInformation
	viaDII.CaptureTime = nil
	if got := SSMHostPlatformStableKey(viaDII); got == key {
		t.Fatalf("inventory and DescribeInstanceInformation evidence key identically: %q", got)
	}
}

func ptr(s string) *string { return &s }
