// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package shield maps AWS Shield Advanced metadata into AWS cloud collector
// facts.
//
// The package owns scanner-level normalization only. It never calls the AWS SDK
// directly and never reads or persists billing detail beyond the subscription
// state and auto-renew flag. SDK adapters provide Protection and Subscription
// values that carry identity and the protected resource ARN; Scanner emits
// aws_shield_protection and aws_shield_subscription resource facts plus a
// protection-to-protected-resource relationship.
//
// Shield is a global service: one claim per account observes every protection
// and the single account subscription regardless of region. The protected
// resource ARN reported by AWS is already partition-correct and is used
// directly (or has its bare id extracted) to key the relationship; the
// helpers.go classifier maps the protected ARN's service segment to one of
// ELBv2 load balancer, CloudFront distribution, Global Accelerator accelerator,
// Elastic IP, or Route 53 hosted zone, and skips emission for any service with
// no canonical Eshu resource family rather than dangling an untyped edge.
package shield
