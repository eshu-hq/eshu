// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// SecurityGroupRule is the schema-version-1 typed payload for the
// "aws_security_group_rule" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// The required set matches the security-group-rule collector emitter
// (awscloud.NewSecurityGroupRuleEnvelope), which validates group_id non-empty
// and always emits account_id, region, direction, ip_protocol, source_kind, and
// source_value from a boundary and a normalized (kind, value) pair. SourceValue
// is required (always emitted) but may be the empty string for the "unknown"
// source kind — an empty required value is a valid observed value the decode
// seam accepts; only an absent key dead-letters. FromPort and ToPort are
// optional pointers because an all-ports rule omits the range (nil stays
// distinct from real port 0), and IsInternet / CorrelationAnchors are optional
// derived fields.
type SecurityGroupRule struct {
	// AccountID is the AWS account the rule was observed in. Required.
	AccountID string `json:"account_id"`

	// Region is the AWS region the rule was observed in. Required.
	Region string `json:"region"`

	// GroupID is the bare security-group id the rule belongs to. Required — it
	// anchors the SecurityGroup CloudResource node the reachability edge hangs
	// off, so a rule with no group id cannot resolve an anchor.
	GroupID string `json:"group_id"`

	// Direction is the normalized rule direction ("ingress" or "egress").
	// Required — it selects the closed-vocabulary relationship type.
	Direction string `json:"direction"`

	// IPProtocol is the rule's IP protocol token (for example "tcp", or "-1"
	// for all protocols). Required — always emitted by the collector.
	IPProtocol string `json:"ip_protocol"`

	// SourceKind is the normalized rule-target family discriminator
	// (cidr_ipv4, cidr_ipv6, prefix_list, referenced_security_group, or
	// unknown). Required — it selects how the reducer resolves the rule's
	// endpoint.
	SourceKind string `json:"source_kind"`

	// SourceValue is the rule target's value for its SourceKind (a CIDR, a
	// prefix-list id, or a referenced group id). Required and always emitted,
	// but the empty string is a valid value for the "unknown" source kind.
	SourceValue string `json:"source_value"`

	// FromPort is the inclusive lower bound of the rule's port range, or nil
	// for an all-ports rule that omits the range. Optional pointer so nil stays
	// distinct from real port 0.
	FromPort *int32 `json:"from_port,omitempty"`

	// ToPort is the inclusive upper bound of the rule's port range, or nil for
	// an all-ports rule that omits the range. Optional pointer so nil stays
	// distinct from real port 0.
	ToPort *int32 `json:"to_port,omitempty"`

	// IsInternet is the collector-derived flag marking a rule whose source is
	// an open-to-the-world CIDR. Optional: it is a convenience property copied
	// onto the rule node, not an identity input.
	IsInternet *bool `json:"is_internet,omitempty"`

	// CorrelationAnchors are the redaction-safe anchors the collector published
	// for the rule. Optional and unused by the reachability extractor's identity
	// path; carried for parity with the emitted payload.
	CorrelationAnchors []string `json:"correlation_anchors,omitempty"`
}
