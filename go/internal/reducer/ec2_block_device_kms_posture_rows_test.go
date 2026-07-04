// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func ec2BlockKMSPostureEnvelope(factID, account, region, instanceID string, volumeIDs ...string) facts.Envelope {
	devices := make([]map[string]any, 0, len(volumeIDs))
	for _, volumeID := range volumeIDs {
		devices = append(devices, map[string]any{
			"device_name": "/dev/sda1",
			"volume_id":   volumeID,
			"status":      "attached",
		})
	}
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.EC2InstancePostureFactKind,
		Payload: map[string]any{
			"account_id":    account,
			"region":        region,
			"resource_type": "aws_ec2_instance",
			"instance_id":   instanceID,
			"block_devices": devices,
		},
	}
}

func ec2BlockKMSVolumeEnvelope(account, region, volumeID string, encrypted any, kmsKeyID string, attachments []map[string]any) facts.Envelope {
	arn := "arn:aws:ec2:" + region + ":" + account + ":volume/" + volumeID
	return facts.Envelope{
		FactKind: facts.AWSResourceFactKind,
		Payload: map[string]any{
			"account_id":    account,
			"region":        region,
			"resource_type": "aws_ec2_volume",
			"resource_id":   volumeID,
			"arn":           arn,
			"attributes": map[string]any{
				"attachments":      attachments,
				"attachment_count": len(attachments),
				"encrypted":        encrypted,
				"kms_key_id":       kmsKeyID,
			},
		},
	}
}

func ec2BlockKMSKeyEnvelope(account, region, keyARN, manager string) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.AWSResourceFactKind,
		Payload: map[string]any{
			"account_id":          account,
			"region":              region,
			"resource_type":       "aws_kms_key",
			"resource_id":         keyARN,
			"arn":                 keyARN,
			"correlation_anchors": []string{keyARN},
			"attributes": map[string]any{
				"key_manager": manager,
			},
		},
	}
}

func ec2BlockKMSRelationship(volumeID, volumeARN, keyARN string) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.AWSRelationshipFactKind,
		Payload: map[string]any{
			"account_id":         "111122223333",
			"region":             "us-east-1",
			"relationship_type":  "ec2_volume_uses_kms_key",
			"source_resource_id": volumeID,
			"source_arn":         volumeARN,
			"target_resource_id": keyARN,
			"target_arn":         keyARN,
			"target_type":        "aws_kms_key",
		},
	}
}

func attachedTo(instanceID, volumeID string) []map[string]any {
	return []map[string]any{{
		"instance_id": instanceID,
		"state":       "attached",
		"volume_id":   volumeID,
	}}
}

func ec2BlockKMSUID(account, region, instanceID string) string {
	return cloudResourceUID(account, region, "aws_ec2_instance", instanceID)
}

func requireEC2BlockKMSRow(t *testing.T, rows []map[string]any, uid string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if row["uid"] == uid {
			return row
		}
	}
	t.Fatalf("missing row for uid %s in %v", uid, rows)
	return nil
}

func TestExtractEC2BlockDeviceKMSPostureRowsDerivesEncryptedWithCustomerKMS(t *testing.T) {
	t.Parallel()

	const account = "111122223333"
	const region = "us-east-1"
	const keyARN = "arn:aws:kms:us-east-1:111122223333:key/customer"
	volumeARN := "arn:aws:ec2:us-east-1:111122223333:volume/vol-a"
	resources := []facts.Envelope{
		ec2BlockKMSVolumeEnvelope(account, region, "vol-a", true, keyARN, attachedTo("i-aaa", "vol-a")),
		ec2BlockKMSKeyEnvelope(account, region, keyARN, "CUSTOMER"),
	}
	relationships := []facts.Envelope{ec2BlockKMSRelationship("vol-a", volumeARN, keyARN)}
	postures := []facts.Envelope{ec2BlockKMSPostureEnvelope("fact-i-aaa", account, region, "i-aaa", "vol-a")}

	rows, tally, _, err := ExtractEC2BlockDeviceKMSPostureRows(resources, relationships, postures)
	if err != nil {
		t.Fatalf("ExtractEC2BlockDeviceKMSPostureRows() error = %v, want nil", err)
	}
	row := requireEC2BlockKMSRow(t, rows, ec2BlockKMSUID(account, region, "i-aaa"))
	if got, want := row["state"], "encrypted"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if got, want := row["reason"], "all_volumes_customer_managed_kms"; got != want {
		t.Fatalf("reason = %v, want %v", got, want)
	}
	if got, want := row["encrypted_volume_count"], int64(1); got != want {
		t.Fatalf("encrypted_volume_count = %v, want %v", got, want)
	}
	if got, want := fmt.Sprint(row["kms_key_ids"]), fmt.Sprint([]string{keyARN}); got != want {
		t.Fatalf("kms_key_ids = %v, want [%s]", row["kms_key_ids"], keyARN)
	}
	if got := tally.decisions["encrypted"]; got != 1 {
		t.Fatalf("tally.decisions[encrypted] = %d, want 1", got)
	}
}

func TestExtractEC2BlockDeviceKMSPostureRowsDerivesUnencryptedAndMixed(t *testing.T) {
	t.Parallel()

	const account = "111122223333"
	const region = "us-east-1"
	const keyARN = "arn:aws:kms:us-east-1:111122223333:key/customer"
	volumeARN := "arn:aws:ec2:us-east-1:111122223333:volume/vol-enc"
	resources := []facts.Envelope{
		ec2BlockKMSVolumeEnvelope(account, region, "vol-plain", false, "", attachedTo("i-plain", "vol-plain")),
		ec2BlockKMSVolumeEnvelope(account, region, "vol-enc", true, keyARN, attachedTo("i-mixed", "vol-enc")),
		ec2BlockKMSVolumeEnvelope(account, region, "vol-unenc", false, "", attachedTo("i-mixed", "vol-unenc")),
		ec2BlockKMSKeyEnvelope(account, region, keyARN, "CUSTOMER"),
	}
	relationships := []facts.Envelope{ec2BlockKMSRelationship("vol-enc", volumeARN, keyARN)}
	postures := []facts.Envelope{
		ec2BlockKMSPostureEnvelope("fact-plain", account, region, "i-plain", "vol-plain"),
		ec2BlockKMSPostureEnvelope("fact-mixed", account, region, "i-mixed", "vol-enc", "vol-unenc"),
	}

	rows, _, _, err := ExtractEC2BlockDeviceKMSPostureRows(resources, relationships, postures)
	if err != nil {
		t.Fatalf("ExtractEC2BlockDeviceKMSPostureRows() error = %v, want nil", err)
	}
	plain := requireEC2BlockKMSRow(t, rows, ec2BlockKMSUID(account, region, "i-plain"))
	if got, want := plain["state"], "not_encrypted"; got != want {
		t.Fatalf("plain state = %v, want %v", got, want)
	}
	mixed := requireEC2BlockKMSRow(t, rows, ec2BlockKMSUID(account, region, "i-mixed"))
	if got, want := mixed["state"], "mixed"; got != want {
		t.Fatalf("mixed state = %v, want %v", got, want)
	}
	if got, want := mixed["unencrypted_volume_count"], int64(1); got != want {
		t.Fatalf("mixed unencrypted count = %v, want %v", got, want)
	}
}

func TestExtractEC2BlockDeviceKMSPostureRowsKeepsUnknownForConservativeCases(t *testing.T) {
	t.Parallel()

	const account = "111122223333"
	const region = "us-east-1"
	const customerKey = "arn:aws:kms:us-east-1:111122223333:key/customer"
	const awsKey = "arn:aws:kms:us-east-1:111122223333:key/aws-managed"
	customerVolumeARN := "arn:aws:ec2:us-east-1:111122223333:volume/vol-missing-key"
	awsVolumeARN := "arn:aws:ec2:us-east-1:111122223333:volume/vol-aws"
	resources := []facts.Envelope{
		ec2BlockKMSVolumeEnvelope(account, region, "vol-missing-key", true, customerKey, attachedTo("i-missing-key", "vol-missing-key")),
		ec2BlockKMSVolumeEnvelope(account, region, "vol-aws", true, awsKey, attachedTo("i-aws-key", "vol-aws")),
		ec2BlockKMSKeyEnvelope(account, region, awsKey, "AWS"),
		ec2BlockKMSVolumeEnvelope(account, region, "vol-detached", true, customerKey, nil),
	}
	relationships := []facts.Envelope{
		ec2BlockKMSRelationship("vol-missing-key", customerVolumeARN, customerKey),
		ec2BlockKMSRelationship("vol-aws", awsVolumeARN, awsKey),
	}
	postures := []facts.Envelope{
		ec2BlockKMSPostureEnvelope("fact-empty", account, region, "i-empty"),
		ec2BlockKMSPostureEnvelope("fact-missing-volume", account, region, "i-missing-volume", "vol-absent"),
		ec2BlockKMSPostureEnvelope("fact-missing-key", account, region, "i-missing-key", "vol-missing-key"),
		ec2BlockKMSPostureEnvelope("fact-aws-key", account, region, "i-aws-key", "vol-aws"),
		ec2BlockKMSPostureEnvelope("fact-detached", account, region, "i-detached", "vol-detached"),
	}

	rows, tally, _, err := ExtractEC2BlockDeviceKMSPostureRows(resources, relationships, postures)
	if err != nil {
		t.Fatalf("ExtractEC2BlockDeviceKMSPostureRows() error = %v, want nil", err)
	}
	cases := []struct {
		instanceID string
		reason     string
	}{
		{instanceID: "i-empty", reason: "no_block_devices"},
		{instanceID: "i-missing-volume", reason: "missing_volume_fact"},
		{instanceID: "i-missing-key", reason: "missing_kms_key_fact"},
		{instanceID: "i-aws-key", reason: "aws_managed_or_default_key"},
		{instanceID: "i-detached", reason: "volume_detached"},
	}
	for _, tc := range cases {
		row := requireEC2BlockKMSRow(t, rows, ec2BlockKMSUID(account, region, tc.instanceID))
		if got, want := row["state"], "unknown"; got != want {
			t.Fatalf("%s state = %v, want %v", tc.instanceID, got, want)
		}
		if got := row["reason"]; got != tc.reason {
			t.Fatalf("%s reason = %v, want %v", tc.instanceID, got, tc.reason)
		}
	}
	if got, want := tally.decisions["unknown"], 5; got != want {
		t.Fatalf("tally.decisions[unknown] = %d, want %d", got, want)
	}
}

func TestExtractEC2BlockDeviceKMSPostureRowsDuplicateReplayAndTombstone(t *testing.T) {
	t.Parallel()

	const account = "111122223333"
	const region = "us-east-1"
	posture := ec2BlockKMSPostureEnvelope("fact-dup", account, region, "i-dup")
	tombstone := ec2BlockKMSPostureEnvelope("fact-tombstone", account, region, "i-dead")
	tombstone.IsTombstone = true

	rows, tally, _, err := ExtractEC2BlockDeviceKMSPostureRows(nil, nil, []facts.Envelope{posture, posture, tombstone})
	if err != nil {
		t.Fatalf("ExtractEC2BlockDeviceKMSPostureRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 duplicate replay row", len(rows))
	}
	if got := tally.skipped["tombstone"]; got != 1 {
		t.Fatalf("tally.skipped[tombstone] = %d, want 1", got)
	}
	if got := tally.decisions["unknown"]; got != 1 {
		t.Fatalf("tally.decisions[unknown] = %d, want 1 deduped decision", got)
	}
}

func TestExtractEC2BlockDeviceKMSPostureRowsConflictingVolumeFactsStayUnknown(t *testing.T) {
	t.Parallel()

	const account = "111122223333"
	const region = "us-east-1"
	const keyARN = "arn:aws:kms:us-east-1:111122223333:key/customer"
	volumeARN := "arn:aws:ec2:us-east-1:111122223333:volume/vol-ambiguous"
	resources := []facts.Envelope{
		ec2BlockKMSVolumeEnvelope(account, region, "vol-ambiguous", false, "", attachedTo("i-ambiguous", "vol-ambiguous")),
		ec2BlockKMSVolumeEnvelope(account, region, "vol-ambiguous", true, keyARN, attachedTo("i-ambiguous", "vol-ambiguous")),
		ec2BlockKMSKeyEnvelope(account, region, keyARN, "CUSTOMER"),
	}
	relationships := []facts.Envelope{ec2BlockKMSRelationship("vol-ambiguous", volumeARN, keyARN)}
	postures := []facts.Envelope{ec2BlockKMSPostureEnvelope("fact-ambiguous", account, region, "i-ambiguous", "vol-ambiguous")}

	rows, tally, _, err := ExtractEC2BlockDeviceKMSPostureRows(resources, relationships, postures)
	if err != nil {
		t.Fatalf("ExtractEC2BlockDeviceKMSPostureRows() error = %v, want nil", err)
	}
	row := requireEC2BlockKMSRow(t, rows, ec2BlockKMSUID(account, region, "i-ambiguous"))
	if got, want := row["state"], "unknown"; got != want {
		t.Fatalf("state = %v, want %v for conflicting volume facts", got, want)
	}
	if got, want := row["reason"], "ambiguous_volume_fact"; got != want {
		t.Fatalf("reason = %v, want %v", got, want)
	}
	if got := tally.decisionReasons[ec2BlockDeviceKMSDecisionKey{outcome: "unknown", reason: "ambiguous_volume_fact"}]; got != 1 {
		t.Fatalf("ambiguous volume decision tally = %d, want 1", got)
	}
}

func TestExtractEC2BlockDeviceKMSPostureRowsConflictingKMSRelationshipsStayUnknown(t *testing.T) {
	t.Parallel()

	const account = "111122223333"
	const region = "us-east-1"
	const firstKeyARN = "arn:aws:kms:us-east-1:111122223333:key/customer-a"
	const secondKeyARN = "arn:aws:kms:us-east-1:111122223333:key/customer-b"
	volumeARN := "arn:aws:ec2:us-east-1:111122223333:volume/vol-conflict"
	resources := []facts.Envelope{
		ec2BlockKMSVolumeEnvelope(account, region, "vol-conflict", true, firstKeyARN, attachedTo("i-conflict", "vol-conflict")),
		ec2BlockKMSKeyEnvelope(account, region, firstKeyARN, "CUSTOMER"),
		ec2BlockKMSKeyEnvelope(account, region, secondKeyARN, "CUSTOMER"),
	}
	relationships := []facts.Envelope{
		ec2BlockKMSRelationship("vol-conflict", volumeARN, firstKeyARN),
		ec2BlockKMSRelationship("vol-conflict", volumeARN, secondKeyARN),
	}
	postures := []facts.Envelope{ec2BlockKMSPostureEnvelope("fact-conflict", account, region, "i-conflict", "vol-conflict")}

	rows, tally, _, err := ExtractEC2BlockDeviceKMSPostureRows(resources, relationships, postures)
	if err != nil {
		t.Fatalf("ExtractEC2BlockDeviceKMSPostureRows() error = %v, want nil", err)
	}
	row := requireEC2BlockKMSRow(t, rows, ec2BlockKMSUID(account, region, "i-conflict"))
	if got, want := row["state"], "unknown"; got != want {
		t.Fatalf("state = %v, want %v for conflicting KMS relationships", got, want)
	}
	if got, want := row["reason"], "ambiguous_kms_relationship"; got != want {
		t.Fatalf("reason = %v, want %v", got, want)
	}
	if got := tally.decisionReasons[ec2BlockDeviceKMSDecisionKey{outcome: "unknown", reason: "ambiguous_kms_relationship"}]; got != 1 {
		t.Fatalf("ambiguous relationship decision tally = %d, want 1", got)
	}
}
