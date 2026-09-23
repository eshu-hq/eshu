// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"errors"
	"fmt"
	"sort"
)

// Class is the pseudonym shape a field's string values take. The policy
// table maps field keys to classes; an unlisted key is ClassUnknown, which is
// treated exactly like ClassOpaque (fail closed) and additionally reported.
type Class int

// The classes, in the order the design table lists them.
const (
	// ClassUnknown is the zero value: the key is not in the policy table. Its
	// values are made opaque and the field path is reported.
	ClassUnknown Class = iota
	// ClassKeep passes the value through unchanged apart from dictionary
	// substitution (enums, regions, digests, timestamps, structural labels).
	ClassKeep
	// ClassAccount is a 12-digit AWS account id, pseudonymized into the
	// reserved 0000xxxxxxxx form so it stays 12 numeric digits.
	ClassAccount
	// ClassARN is an ARN whose partition, service, region, leading resource
	// type token and numeric qualifiers are kept while the account and every
	// other path component are pseudonymized.
	ClassARN
	// ClassIdent is a resource name or composite id whose shape is sniffed
	// (ARN, ECR reference, AWS-issued id, 32-hex id, account, address,
	// hostname, name:revision) and pseudonymized accordingly.
	ClassIdent
	// ClassAWSID is an AWS-issued id (i-, ami-, subnet-, sg-, eni-, vol-, ...):
	// the prefix is kept and the hex run replaced by HMAC hex of equal length.
	ClassAWSID
	// ClassHost is a DNS name: customer labels become h+10hex, the public
	// suffix collapses to .example, AWS-owned suffixes are kept.
	ClassHost
	// ClassECRRef is a container image reference: host per ClassHost,
	// repository path per ClassIdent, tag and digest kept.
	ClassECRRef
	// ClassIPv4 maps an address into the RFC 5737 documentation ranges.
	ClassIPv4
	// ClassIPv6 maps an address into the RFC 3849 2001:db8::/32 block.
	ClassIPv6
	// ClassCIDR pseudonymizes the network address per ClassIPv4/IPv6 and keeps
	// the prefix length; 0.0.0.0/0, ::/0 and loopback are kept.
	ClassCIDR
	// ClassTagValue is a free-form tag value: t+11hex.
	ClassTagValue
	// ClassEmail becomes <11hex>@example.com.
	ClassEmail
	// ClassOpaque replaces the value with o+11hex and reports the path.
	ClassOpaque
)

var classNames = map[Class]string{
	ClassUnknown:  "unknown",
	ClassKeep:     "keep",
	ClassAccount:  "account",
	ClassARN:      "arn",
	ClassIdent:    "ident",
	ClassAWSID:    "aws_id",
	ClassHost:     "host",
	ClassECRRef:   "image_ref",
	ClassIPv4:     "ipv4",
	ClassIPv6:     "ipv6",
	ClassCIDR:     "cidr",
	ClassTagValue: "tag_value",
	ClassEmail:    "email",
	ClassOpaque:   "opaque",
}

// String returns the class's log label.
func (c Class) String() string {
	if name, ok := classNames[c]; ok {
		return name
	}
	return fmt.Sprintf("class(%d)", int(c))
}

// Policy is the fail-closed field table: JSON object key -> Class. It is
// collector-owned (the aws table lives in collector/awscloud/recordpolicy);
// the engine is collector-neutral. A key absent from Fields is ClassUnknown.
type Policy struct {
	// Fields maps a JSON object key, at any depth of a payload, to its class.
	Fields map[string]Class
}

// Validate rejects an empty table and any entry that explicitly maps to
// ClassUnknown (the table must say Keep or Opaque, never "not sure").
func (p Policy) Validate() error {
	if len(p.Fields) == 0 {
		return errors.New("recordpseudo: policy has no fields")
	}
	var bad []string
	for key, class := range p.Fields {
		if class == ClassUnknown {
			bad = append(bad, key)
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return fmt.Errorf("recordpseudo: policy maps %d field(s) to the unknown class: %v", len(bad), bad)
	}
	return nil
}

// Clone returns an independent copy so a caller can adjust one field without
// mutating the collector's shared table.
func (p Policy) Clone() Policy {
	out := Policy{Fields: make(map[string]Class, len(p.Fields))}
	for key, class := range p.Fields {
		out.Fields[key] = class
	}
	return out
}

// classOf resolves a key; absent keys are ClassUnknown.
func (p Policy) classOf(key string) Class {
	return p.Fields[key]
}

// Config is what a recorder needs to pseudonymize one run.
type Config struct {
	// Key is the corpus recording key. Required.
	Key Key
	// Policy is the collector's field table. Required.
	Policy Policy
}

// Validate reports a missing key or an invalid policy.
func (c Config) Validate() error {
	if c.Key.IsZero() {
		return errors.New("recordpseudo: recording key is required")
	}
	return c.Policy.Validate()
}
