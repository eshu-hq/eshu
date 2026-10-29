// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// TestSSMHostApplicationJSONSpelling pins the exact v1.0.0 wire keys for
// eshu-hq/eshu#9556: the EC2 host tuple, the inventory source envelope, the
// capture time, and exactly the six allowlisted AWS:Application row fields.
// No URL, Summary, hostname, IP, PURL, image digest, vulnerability, or
// supply-chain key may appear on this wire shape.
func TestSSMHostApplicationJSONSpelling(t *testing.T) {
	t.Parallel()

	name, version, release, epoch, packageID, arch := "openssl", "3.0.8", "1.amzn2023", "1", "openssl-3.0.8-1.amzn2023.x86_64", "x86_64"
	collector := "collector-1"
	app := SSMHostApplication{
		Partition:           "aws",
		AccountID:           "055929692761",
		Region:              "us-east-1",
		InstanceID:          "i-0e18f7b9871b85f57",
		ResourceType:        "AWS::EC2::Instance",
		SourceType:          SSMInventoryTypeApplication,
		SchemaVersion:       "1.0.0",
		CaptureTime:         "2026-10-29T22:31:19Z",
		Name:                &name,
		Version:             &version,
		Release:             &release,
		Epoch:               &epoch,
		PackageID:           &packageID,
		Architecture:        &arch,
		CollectorInstanceID: &collector,
	}

	raw, err := json.Marshal(app)
	if err != nil {
		t.Fatalf("json.Marshal(SSMHostApplication) error = %v", err)
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
		"account_id", "architecture", "capture_time", "collector_instance_id",
		"epoch", "instance_id", "name", "package_id", "partition",
		"region", "release", "resource_type", "schema_version",
		"source_type", "version",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("wire keys = %v, want %v", got, want)
	}

	for _, forbidden := range []string{
		"url", "summary", "hostname", "ip_address", "purl",
		"image_digest", "vulnerability", "supply_chain_impact",
		"advisory", "cve",
	} {
		if _, ok := decoded[forbidden]; ok {
			t.Fatalf("wire payload carries forbidden key %q", forbidden)
		}
	}
}

// TestSSMHostApplicationOptionalRowFieldsOmitted proves the six allowlisted
// row fields are optional-when-present: a row observed without them still
// marshals to the host tuple plus source envelope only.
func TestSSMHostApplicationOptionalRowFieldsOmitted(t *testing.T) {
	t.Parallel()

	app := SSMHostApplication{
		Partition:     "aws",
		AccountID:     "055929692761",
		Region:        "us-east-1",
		InstanceID:    "i-0e18f7b9871b85f57",
		ResourceType:  "AWS::EC2::Instance",
		SourceType:    SSMInventoryTypeApplication,
		SchemaVersion: "1.0.0",
		CaptureTime:   "2026-10-29T22:31:19Z",
	}
	raw, err := json.Marshal(app)
	if err != nil {
		t.Fatalf("json.Marshal(SSMHostApplication) error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(marshaled) error = %v", err)
	}
	for _, key := range []string{"name", "version", "release", "epoch", "package_id", "architecture", "collector_instance_id"} {
		if _, ok := decoded[key]; ok {
			t.Fatalf("optional key %q present with nil field", key)
		}
	}
}

// TestSSMHostApplicationCanonicalRowHashIsAllowlistOnly proves the stable-key
// input for eshu-hq/eshu#9556: identical allowlisted rows hash identically
// even when the surrounding envelope differs (another capture's collector
// run, a reordered struct literal), while any allowlisted field difference
// changes the hash. Non-allowlisted content can never affect identity.
func TestSSMHostApplicationCanonicalRowHashIsAllowlistOnly(t *testing.T) {
	t.Parallel()

	name, version := "openssl", "3.0.8"
	base := SSMHostApplication{
		Partition: "aws", AccountID: "055929692761", Region: "us-east-1",
		InstanceID: "i-0e18f7b9871b85f57", ResourceType: "AWS::EC2::Instance",
		SourceType: SSMInventoryTypeApplication, SchemaVersion: "1.0.0",
		CaptureTime: "2026-10-29T22:31:19Z", Name: &name, Version: &version,
	}
	collectorA, collectorB := "collector-1", "collector-2"
	withA := base
	withA.CollectorInstanceID = &collectorA
	withB := base
	withB.CollectorInstanceID = &collectorB

	hashA := CanonicalSSMHostApplicationRow(withA)
	hashB := CanonicalSSMHostApplicationRow(withB)
	if len(hashA) != 64 || len(hashB) != 64 {
		t.Fatalf("canonical hashes = %q, %q, want 64 lowercase hex each", hashA, hashB)
	}
	if hashA != hashB {
		t.Fatalf("same allowlisted row hashes differ: %q vs %q", hashA, hashB)
	}

	other := base
	otherName := "curl"
	other.Name = &otherName
	if got := CanonicalSSMHostApplicationRow(other); got == hashA {
		t.Fatalf("different Name hashes equal: %q", got)
	}

	empty := SSMHostApplication{}
	if got := CanonicalSSMHostApplicationRow(empty); got == hashA {
		t.Fatalf("empty row hashes equal to populated row: %q", got)
	}
}

// TestSSMHostApplicationStableKeyComposition proves the stable key carries
// the host tuple, the capture time, and the canonical row hash: the same row
// in another capture keys differently, while a collector-run-only difference
// keys identically (deduplication within one capture).
func TestSSMHostApplicationStableKeyComposition(t *testing.T) {
	t.Parallel()

	name := "openssl"
	base := SSMHostApplication{
		Partition: "aws", AccountID: "055929692761", Region: "us-east-1",
		InstanceID: "i-0e18f7b9871b85f57", ResourceType: "AWS::EC2::Instance",
		SourceType: SSMInventoryTypeApplication, SchemaVersion: "1.0.0",
		CaptureTime: "2026-10-29T22:31:19Z", Name: &name,
	}
	key := SSMHostApplicationStableKey(base)
	for _, want := range []string{
		"aws_ssm_host_application", "aws", "055929692761", "us-east-1",
		"i-0e18f7b9871b85f57", "2026-10-29T22:31:19Z",
		CanonicalSSMHostApplicationRow(base),
	} {
		if !strings.Contains(key, want) {
			t.Fatalf("stable key %q misses %q", key, want)
		}
	}

	recapture := base
	recapture.CaptureTime = "2026-10-30T22:31:19Z"
	if got := SSMHostApplicationStableKey(recapture); got == key {
		t.Fatalf("another capture keys identically: %q", got)
	}

	rerun := base
	rerunCollector := "collector-2"
	rerun.CollectorInstanceID = &rerunCollector
	if got := SSMHostApplicationStableKey(rerun); got != key {
		t.Fatalf("collector-run-only difference changes key: %q vs %q", got, key)
	}
}
