// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package route53resolver emits AWS Route 53 Resolver metadata facts for one
// claimed account and region.
//
// The scanner covers resolver endpoints (identity, direction, status, IP
// count), resolver rules (name, domain name, rule type) and rule associations,
// DNS Firewall rule groups (rule count only), firewall domain lists (domain
// count only), firewall rule group associations, and query log configurations
// (destination ARN only). It emits aws_resource and aws_relationship facts;
// relationship edges cross to EC2-owned VPCs and subnets by AWS-reported
// identifier.
//
// The package is metadata-only. It never reads DNS Firewall domain list
// contents, never reads query log records, never carries resolver endpoint IP
// address strings, and never mutates a resource. Counts for firewall rule
// groups and domain lists come from the per-resource Get reads, which return
// only metadata and an aggregate count. Callers must handle client errors,
// which the scanner wraps with %w and never swallows.
package route53resolver
