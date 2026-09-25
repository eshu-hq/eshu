// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wafv2

import "context"

// Client is the WAFv2 read surface consumed by Scanner. Runtime adapters
// translate AWS SDK responses into these scanner-owned metadata records. The
// adapter must select scope (REGIONAL or CLOUDFRONT) from the claim boundary
// and must never request or return IP address lists, regex pattern bodies, or
// rule Statement bodies.
type Client interface {
	// ListWebACLs returns web ACL metadata, association references, and the
	// reference ARNs needed to project relationships. Implementations resolve
	// rule reference ARNs and managed rule set references from the web ACL
	// rules without persisting any Statement body.
	ListWebACLs(context.Context) ([]WebACL, error)
	// ListRuleGroups returns customer-owned rule group metadata. Managed
	// (AWS/marketplace) rule groups are not customer resources and are not
	// returned here; they appear as managed rule set references on web ACLs.
	ListRuleGroups(context.Context) ([]RuleGroup, error)
	// ListIPSets returns IP set metadata including the address count. The
	// address list itself is never returned.
	ListIPSets(context.Context) ([]IPSet, error)
	// ListRegexPatternSets returns regex pattern set metadata including the
	// pattern count. The regex bodies are never returned.
	ListRegexPatternSets(context.Context) ([]RegexPatternSet, error)
}

// WebACL is the scanner-owned representation of one WAFv2 web ACL. It carries
// identity and aggregate metadata plus the reference ARNs needed to project
// relationships. Rule Statement bodies are intentionally outside this contract.
type WebACL struct {
	ARN                string
	ID                 string
	Name               string
	Description        string
	Scope              string
	RuleCount          int
	Capacity           int64
	ManagedByFirewall  bool
	DefaultAction      string
	Tags               map[string]string
	ManagedRuleSetRefs []ManagedRuleSetRef
	RuleGroupRefARNs   []string
	IPSetRefARNs       []string
	RegexSetRefARNs    []string
	ProtectedResources []ProtectedResource
}

// ManagedRuleSetRef names one managed rule group referenced by a web ACL. Only
// the vendor and rule group name are persisted; the rule contents stay with
// the managing vendor and are never read.
type ManagedRuleSetRef struct {
	VendorName string
	Name       string
	Version    string
}

// ProtectedResource is one resource a web ACL is associated with. WAFv2 reports
// CloudFront associations on the web ACL and regional associations through
// ListResourcesForWebACL. The scanner records the ARN and AWS-reported resource
// type only.
type ProtectedResource struct {
	ARN          string
	ResourceType string
}

// RuleGroup is the scanner-owned representation of one customer-owned WAFv2 rule
// group. The rule Statement bodies are intentionally outside this contract; the
// scanner persists identity and the rule count only.
type RuleGroup struct {
	ARN         string
	ID          string
	Name        string
	Description string
	Scope       string
	RuleCount   int
	Capacity    int64
	Tags        map[string]string
}

// IPSet is the scanner-owned representation of one WAFv2 IP set. AddressCount is
// the number of CIDR entries; the entries themselves are never persisted
// because they commonly include private CIDR ranges and threat-intel data.
type IPSet struct {
	ARN          string
	ID           string
	Name         string
	Description  string
	Scope        string
	IPVersion    string
	AddressCount int
	Tags         map[string]string
}

// RegexPatternSet is the scanner-owned representation of one WAFv2 regex pattern
// set. PatternCount is the number of regex entries; the regex bodies are never
// persisted because they encode customer-detection rules.
type RegexPatternSet struct {
	ARN          string
	ID           string
	Name         string
	Description  string
	Scope        string
	PatternCount int
	Tags         map[string]string
}
