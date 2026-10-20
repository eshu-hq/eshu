// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package wafv2 maps AWS WAFv2 metadata into AWS cloud collector facts.
//
// The package owns scanner-level normalization only. It never calls the AWS
// SDK directly and never persists IP set address lists, regex pattern bodies,
// or rule Statement bodies. SDK adapters provide WebACL, RuleGroup, IPSet, and
// RegexPatternSet values that carry counts and reference identity but no
// sensitive payloads; Scanner emits aws_resource facts plus web-ACL-to-rule
// group, web-ACL-to-IP set, web-ACL-to-regex set, and web-ACL-to-protected
// resource relationship evidence. WAF Classic (v1) is out of scope.
package wafv2
