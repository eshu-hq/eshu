// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	ec2ExposureAccount = "111122223333"
	ec2ExposureRegion  = "us-east-1"
)

func ec2ExposurePostureEnvelope(factID, instanceID string, payload map[string]any) facts.Envelope {
	merged := map[string]any{
		"account_id":     ec2ExposureAccount,
		"region":         ec2ExposureRegion,
		"resource_type":  "aws_ec2_instance",
		"instance_id":    instanceID,
		"service_kind":   "ec2",
		"source_fact_id": factID,
	}
	for key, value := range payload {
		merged[key] = value
	}
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.EC2InstancePostureFactKind,
		Payload:  merged,
	}
}

func ec2ExposureRelationshipEnvelope(factID, relType, sourceID, targetID, targetType string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.AWSRelationshipFactKind,
		Payload: map[string]any{
			"account_id":           ec2ExposureAccount,
			"region":               ec2ExposureRegion,
			"relationship_type":    relType,
			"source_resource_id":   sourceID,
			"target_resource_id":   targetID,
			"target_type":          targetType,
			"collector_kind":       "aws",
			"collector_instance":   "test",
			"collector_generation": "gen-1",
		},
	}
}

func ec2ExposureSecurityGroupRuleEnvelope(factID, groupID, direction string, isInternet bool) facts.Envelope {
	sourceValue := "10.0.0.0/8"
	if isInternet {
		sourceValue = "0.0.0.0/0"
	}
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.AWSSecurityGroupRuleFactKind,
		Payload: map[string]any{
			"account_id":     ec2ExposureAccount,
			"region":         ec2ExposureRegion,
			"group_id":       groupID,
			"direction":      direction,
			"ip_protocol":    "tcp",
			"from_port":      int32(443),
			"to_port":        int32(443),
			"source_kind":    "cidr_ipv4",
			"source_value":   sourceValue,
			"is_internet":    isInternet,
			"service_kind":   "ec2",
			"collector_kind": "aws",
		},
	}
}

func requireEC2InternetExposureRow(t *testing.T, rows []map[string]any, uid string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if row["uid"] == uid {
			return row
		}
	}
	t.Fatalf("no ec2 internet-exposure row found for uid %q in %v", uid, rows)
	return nil
}

func ec2ExposureUID(instanceID string) string {
	return cloudResourceUID(ec2ExposureAccount, ec2ExposureRegion, "aws_ec2_instance", instanceID)
}

func TestExtractEC2InternetExposureRowsDerivesExposedFromPublicIPAndInternetReachableSG(t *testing.T) {
	t.Parallel()

	postures := []facts.Envelope{ec2ExposurePostureEnvelope("fact-posture-1", "i-123", map[string]any{
		"public_ip_associated": true,
		"public_ip_address":    "203.0.113.10",
	})}
	relationships := []facts.Envelope{
		ec2ExposureRelationshipEnvelope("fact-eni-instance", "ec2_network_interface_attached_to_resource", "eni-1", "i-123", "aws_ec2_instance"),
		ec2ExposureRelationshipEnvelope("fact-eni-sg", "ec2_network_interface_uses_security_group", "eni-1", "sg-1", "aws_ec2_security_group"),
	}
	rules := []facts.Envelope{ec2ExposureSecurityGroupRuleEnvelope("fact-sg-rule", "sg-1", "ingress", true)}

	rows, tally, err := ExtractEC2InternetExposureRows(postures, relationships, rules)
	if err != nil {
		t.Fatalf("ExtractEC2InternetExposureRows() error = %v, want nil", err)
	}
	row := requireEC2InternetExposureRow(t, rows, ec2ExposureUID("i-123"))
	if got, want := row["state"], "exposed"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if got, want := row["internet_exposed"], true; got != want {
		t.Fatalf("internet_exposed = %v, want %v", got, want)
	}
	if got, want := row["reason"], "public_ip_reachable_from_internet_sg"; got != want {
		t.Fatalf("reason = %v, want %v", got, want)
	}
	if _, leaked := row["public_ip_address"]; leaked {
		t.Fatalf("row leaked raw public IP: %v", row)
	}
	if got, want := tally.decisions["exposed"], 1; got != want {
		t.Fatalf("decisions[exposed] = %d, want %d", got, want)
	}
}

func TestExtractEC2InternetExposureRowsDerivesNotExposedWithoutPublicIP(t *testing.T) {
	t.Parallel()

	postures := []facts.Envelope{ec2ExposurePostureEnvelope("fact-posture-1", "i-123", map[string]any{
		"public_ip_associated": false,
	})}

	rows, _, err := ExtractEC2InternetExposureRows(postures, nil, nil)
	if err != nil {
		t.Fatalf("ExtractEC2InternetExposureRows() error = %v, want nil", err)
	}
	row := requireEC2InternetExposureRow(t, rows, ec2ExposureUID("i-123"))
	if got, want := row["state"], "not_exposed"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if got, want := row["internet_exposed"], false; got != want {
		t.Fatalf("internet_exposed = %v, want %v", got, want)
	}
	if got, want := row["reason"], "no_public_ip"; got != want {
		t.Fatalf("reason = %v, want %v", got, want)
	}
}

func TestExtractEC2InternetExposureRowsKeepsUnknownWhenPublicIPStateUnknown(t *testing.T) {
	t.Parallel()

	postures := []facts.Envelope{ec2ExposurePostureEnvelope("fact-posture-1", "i-123", nil)}

	rows, _, err := ExtractEC2InternetExposureRows(postures, nil, nil)
	if err != nil {
		t.Fatalf("ExtractEC2InternetExposureRows() error = %v, want nil", err)
	}
	row := requireEC2InternetExposureRow(t, rows, ec2ExposureUID("i-123"))
	if got, want := row["state"], "unknown"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if got := row["internet_exposed"]; got != nil {
		t.Fatalf("internet_exposed = %v, want nil for unknown", got)
	}
	if got, want := row["reason"], "public_ip_unknown"; got != want {
		t.Fatalf("reason = %v, want %v", got, want)
	}
}

func TestExtractEC2InternetExposureRowsKeepsUnknownForPublicIPWithoutENIAttachment(t *testing.T) {
	t.Parallel()

	postures := []facts.Envelope{ec2ExposurePostureEnvelope("fact-posture-1", "i-123", map[string]any{
		"public_ip_associated": true,
	})}

	rows, _, err := ExtractEC2InternetExposureRows(postures, nil, nil)
	if err != nil {
		t.Fatalf("ExtractEC2InternetExposureRows() error = %v, want nil", err)
	}
	row := requireEC2InternetExposureRow(t, rows, ec2ExposureUID("i-123"))
	if got, want := row["state"], "unknown"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if got, want := row["reason"], "eni_attachment_unresolved"; got != want {
		t.Fatalf("reason = %v, want %v", got, want)
	}
}

func TestExtractEC2InternetExposureRowsKeepsUnknownForPublicIPWithoutSecurityGroupAttachment(t *testing.T) {
	t.Parallel()

	postures := []facts.Envelope{ec2ExposurePostureEnvelope("fact-posture-1", "i-123", map[string]any{
		"public_ip_associated": true,
	})}
	relationships := []facts.Envelope{
		ec2ExposureRelationshipEnvelope("fact-eni-instance", "ec2_network_interface_attached_to_resource", "eni-1", "i-123", "aws_ec2_instance"),
	}

	rows, _, err := ExtractEC2InternetExposureRows(postures, relationships, nil)
	if err != nil {
		t.Fatalf("ExtractEC2InternetExposureRows() error = %v, want nil", err)
	}
	row := requireEC2InternetExposureRow(t, rows, ec2ExposureUID("i-123"))
	if got, want := row["state"], "unknown"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if got, want := row["reason"], "security_group_attachment_unresolved"; got != want {
		t.Fatalf("reason = %v, want %v", got, want)
	}
}

func TestExtractEC2InternetExposureRowsKeepsUnknownForPublicIPWithoutObservedIngressRules(t *testing.T) {
	t.Parallel()

	postures := []facts.Envelope{ec2ExposurePostureEnvelope("fact-posture-1", "i-123", map[string]any{
		"public_ip_associated": true,
	})}
	relationships := []facts.Envelope{
		ec2ExposureRelationshipEnvelope("fact-eni-instance", "ec2_network_interface_attached_to_resource", "eni-1", "i-123", "aws_ec2_instance"),
		ec2ExposureRelationshipEnvelope("fact-eni-sg", "ec2_network_interface_uses_security_group", "eni-1", "sg-1", "aws_ec2_security_group"),
	}

	rows, _, err := ExtractEC2InternetExposureRows(postures, relationships, nil)
	if err != nil {
		t.Fatalf("ExtractEC2InternetExposureRows() error = %v, want nil", err)
	}
	row := requireEC2InternetExposureRow(t, rows, ec2ExposureUID("i-123"))
	if got, want := row["state"], "unknown"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if got, want := row["reason"], "reachability_unresolved"; got != want {
		t.Fatalf("reason = %v, want %v", got, want)
	}
}

func TestExtractEC2InternetExposureRowsDerivesNotExposedForPrivateOnlyIngress(t *testing.T) {
	t.Parallel()

	postures := []facts.Envelope{ec2ExposurePostureEnvelope("fact-posture-1", "i-123", map[string]any{
		"public_ip_associated": true,
	})}
	relationships := []facts.Envelope{
		ec2ExposureRelationshipEnvelope("fact-eni-instance", "ec2_network_interface_attached_to_resource", "eni-1", "i-123", "aws_ec2_instance"),
		ec2ExposureRelationshipEnvelope("fact-eni-sg", "ec2_network_interface_uses_security_group", "eni-1", "sg-1", "aws_ec2_security_group"),
	}
	rules := []facts.Envelope{ec2ExposureSecurityGroupRuleEnvelope("fact-sg-rule", "sg-1", "ingress", false)}

	rows, _, err := ExtractEC2InternetExposureRows(postures, relationships, rules)
	if err != nil {
		t.Fatalf("ExtractEC2InternetExposureRows() error = %v, want nil", err)
	}
	row := requireEC2InternetExposureRow(t, rows, ec2ExposureUID("i-123"))
	if got, want := row["state"], "not_exposed"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if got, want := row["internet_exposed"], false; got != want {
		t.Fatalf("internet_exposed = %v, want %v", got, want)
	}
	if got, want := row["reason"], "no_internet_reachable_sg"; got != want {
		t.Fatalf("reason = %v, want %v", got, want)
	}
}

func TestExtractEC2InternetExposureRowsSkipsMissingIdentity(t *testing.T) {
	t.Parallel()

	postures := []facts.Envelope{ec2ExposurePostureEnvelope("fact-posture-1", "", map[string]any{
		"public_ip_associated": true,
	})}

	rows, tally, err := ExtractEC2InternetExposureRows(postures, nil, nil)
	if err != nil {
		t.Fatalf("ExtractEC2InternetExposureRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for missing identity", len(rows))
	}
	if got, want := tally.skipped[ec2InternetExposureSkipMissingIdentity], 1; got != want {
		t.Fatalf("skipped[missing_identity] = %d, want %d", got, want)
	}
}

func TestExtractEC2InternetExposureRowsSkipsTombstone(t *testing.T) {
	t.Parallel()

	tombstone := ec2ExposurePostureEnvelope("fact-posture-1", "i-123", map[string]any{
		"public_ip_associated": true,
	})
	tombstone.IsTombstone = true

	rows, tally, err := ExtractEC2InternetExposureRows([]facts.Envelope{tombstone}, nil, nil)
	if err != nil {
		t.Fatalf("ExtractEC2InternetExposureRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for tombstone", len(rows))
	}
	if got, want := tally.skipped[ec2InternetExposureSkipTombstone], 1; got != want {
		t.Fatalf("skipped[tombstone] = %d, want %d", got, want)
	}
}
