// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func assertPayloadBool(t *testing.T, payload map[string]any, key string, want bool) {
	t.Helper()
	got, ok := payload[key].(bool)
	if !ok {
		t.Fatalf("payload[%q] = %T, want bool", key, payload[key])
	}
	if got != want {
		t.Fatalf("payload[%q] = %v, want %v", key, got, want)
	}
}

func securityGroupRuleBoundary(observedAt time.Time) Boundary {
	boundary := testBoundary(observedAt)
	boundary.Region = "us-east-1"
	boundary.ServiceKind = ServiceEC2
	boundary.ScopeID = "aws:123456789012:us-east-1"
	boundary.GenerationID = "aws:123456789012:us-east-1:ec2:1"
	return boundary
}

func TestNewSecurityGroupRuleEnvelopeProvenance(t *testing.T) {
	t.Parallel()

	fromPort := int32(443)
	toPort := int32(443)
	envelope, err := NewSecurityGroupRuleEnvelope(SecurityGroupRuleObservation{
		Boundary:     securityGroupRuleBoundary(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)),
		RuleID:       "sgr-123",
		GroupID:      "sg-123",
		GroupOwnerID: "123456789012",
		IsEgress:     false,
		IPProtocol:   "tcp",
		FromPort:     &fromPort,
		ToPort:       &toPort,
		CIDRIPv4:     "0.0.0.0/0",
		Description:  "public https",
	})
	if err != nil {
		t.Fatalf("NewSecurityGroupRuleEnvelope returned error: %v", err)
	}
	if envelope.FactKind != facts.AWSSecurityGroupRuleFactKind {
		t.Fatalf("FactKind = %q, want %q", envelope.FactKind, facts.AWSSecurityGroupRuleFactKind)
	}
	if envelope.SchemaVersion != facts.AWSSecurityGroupRuleSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", envelope.SchemaVersion, facts.AWSSecurityGroupRuleSchemaVersion)
	}
	if envelope.CollectorKind != CollectorKind {
		t.Fatalf("CollectorKind = %q, want %q", envelope.CollectorKind, CollectorKind)
	}
	if envelope.SourceConfidence != facts.SourceConfidenceReported {
		t.Fatalf("SourceConfidence = %q, want %q", envelope.SourceConfidence, facts.SourceConfidenceReported)
	}
	assertPayloadString(t, envelope.Payload, "group_id", "sg-123")
	assertPayloadString(t, envelope.Payload, "direction", SecurityGroupRuleDirectionIngress)
	assertPayloadString(t, envelope.Payload, "ip_protocol", "tcp")
	assertPayloadString(t, envelope.Payload, "source_kind", SecurityGroupRuleSourceCIDRIPv4)
	assertPayloadString(t, envelope.Payload, "source_value", "0.0.0.0/0")
	assertPayloadString(t, envelope.Payload, "service_kind", ServiceEC2)
	assertPayloadBool(t, envelope.Payload, "is_internet", true)
	assertPayloadBool(t, envelope.Payload, "is_all_protocols", false)
	assertPayloadBool(t, envelope.Payload, "is_all_ports", false)
	if got := envelope.Payload["from_port"]; got != int32(443) {
		t.Fatalf("from_port = %#v, want int32(443)", got)
	}
}

func TestNewSecurityGroupRuleEnvelopeNormalizesSource(t *testing.T) {
	t.Parallel()

	boundary := securityGroupRuleBoundary(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC))
	cases := []struct {
		name           string
		mutate         func(*SecurityGroupRuleObservation)
		wantKind       string
		wantValue      string
		wantIsInternet bool
	}{
		{
			name:           "ipv4 internet",
			mutate:         func(o *SecurityGroupRuleObservation) { o.CIDRIPv4 = "0.0.0.0/0" },
			wantKind:       SecurityGroupRuleSourceCIDRIPv4,
			wantValue:      "0.0.0.0/0",
			wantIsInternet: true,
		},
		{
			name:           "ipv4 scoped",
			mutate:         func(o *SecurityGroupRuleObservation) { o.CIDRIPv4 = "10.0.0.0/8" },
			wantKind:       SecurityGroupRuleSourceCIDRIPv4,
			wantValue:      "10.0.0.0/8",
			wantIsInternet: false,
		},
		{
			name:           "ipv6 internet",
			mutate:         func(o *SecurityGroupRuleObservation) { o.CIDRIPv6 = "::/0" },
			wantKind:       SecurityGroupRuleSourceCIDRIPv6,
			wantValue:      "::/0",
			wantIsInternet: true,
		},
		{
			name:           "prefix list",
			mutate:         func(o *SecurityGroupRuleObservation) { o.PrefixListID = "pl-123" },
			wantKind:       SecurityGroupRuleSourcePrefixList,
			wantValue:      "pl-123",
			wantIsInternet: false,
		},
		{
			name:           "referenced group",
			mutate:         func(o *SecurityGroupRuleObservation) { o.ReferencedSG = "sg-peer" },
			wantKind:       SecurityGroupRuleSourceSecurityGroup,
			wantValue:      "sg-peer",
			wantIsInternet: false,
		},
		{
			name:           "no target",
			mutate:         func(o *SecurityGroupRuleObservation) {},
			wantKind:       SecurityGroupRuleSourceUnknown,
			wantValue:      "",
			wantIsInternet: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observation := SecurityGroupRuleObservation{
				Boundary:   boundary,
				RuleID:     "sgr-" + tc.name,
				GroupID:    "sg-123",
				IPProtocol: "tcp",
			}
			tc.mutate(&observation)
			envelope, err := NewSecurityGroupRuleEnvelope(observation)
			if err != nil {
				t.Fatalf("NewSecurityGroupRuleEnvelope returned error: %v", err)
			}
			assertPayloadString(t, envelope.Payload, "source_kind", tc.wantKind)
			assertPayloadString(t, envelope.Payload, "source_value", tc.wantValue)
			assertPayloadBool(t, envelope.Payload, "is_internet", tc.wantIsInternet)
		})
	}
}

func TestNewSecurityGroupRuleEnvelopeAllProtocols(t *testing.T) {
	t.Parallel()

	envelope, err := NewSecurityGroupRuleEnvelope(SecurityGroupRuleObservation{
		Boundary:   securityGroupRuleBoundary(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)),
		RuleID:     "sgr-all",
		GroupID:    "sg-123",
		IsEgress:   true,
		IPProtocol: "-1",
		CIDRIPv4:   "0.0.0.0/0",
	})
	if err != nil {
		t.Fatalf("NewSecurityGroupRuleEnvelope returned error: %v", err)
	}
	assertPayloadString(t, envelope.Payload, "direction", SecurityGroupRuleDirectionEgress)
	assertPayloadBool(t, envelope.Payload, "is_all_protocols", true)
	assertPayloadBool(t, envelope.Payload, "is_all_ports", true)
	if got := envelope.Payload["from_port"]; got != nil {
		t.Fatalf("from_port = %#v, want nil for all-protocols rule", got)
	}
}

func TestNewSecurityGroupRuleEnvelopeRequiresGroupID(t *testing.T) {
	t.Parallel()

	_, err := NewSecurityGroupRuleEnvelope(SecurityGroupRuleObservation{
		Boundary:   securityGroupRuleBoundary(time.Now()),
		RuleID:     "sgr-123",
		IPProtocol: "tcp",
	})
	if err == nil {
		t.Fatal("NewSecurityGroupRuleEnvelope returned nil error, want missing group_id error")
	}
}

func TestNewSecurityGroupRuleEnvelopeStableIdentity(t *testing.T) {
	t.Parallel()

	build := func() facts.Envelope {
		fromPort := int32(22)
		toPort := int32(22)
		envelope, err := NewSecurityGroupRuleEnvelope(SecurityGroupRuleObservation{
			Boundary:   securityGroupRuleBoundary(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)),
			RuleID:     "sgr-ssh",
			GroupID:    "sg-123",
			IPProtocol: "tcp",
			FromPort:   &fromPort,
			ToPort:     &toPort,
			CIDRIPv4:   "10.0.0.0/8",
		})
		if err != nil {
			t.Fatalf("NewSecurityGroupRuleEnvelope returned error: %v", err)
		}
		return envelope
	}
	first := build()
	second := build()
	if first.StableFactKey != second.StableFactKey {
		t.Fatalf("stable fact key not deterministic: %q != %q", first.StableFactKey, second.StableFactKey)
	}
	if first.FactID != second.FactID {
		t.Fatalf("fact id not deterministic: %q != %q", first.FactID, second.FactID)
	}
}
