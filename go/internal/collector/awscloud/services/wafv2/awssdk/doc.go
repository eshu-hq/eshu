// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts AWS SDK for Go v2 WAFv2 calls into scanner-owned
// metadata.
//
// The adapter calls only read operations: ListWebACLs, GetWebACL,
// ListResourcesForWebACL, ListRuleGroups, GetRuleGroup, ListIPSets, GetIPSet,
// ListRegexPatternSets, GetRegexPatternSet, and ListTagsForResource. Its
// apiClient interface exposes no mutation or data-plane method, and a
// reflection gate fails the build path if one is added. The adapter counts IP
// set addresses and regex patterns and discards the bodies, walks web ACL
// rules to collect only reference ARNs and managed rule group vendor/name, and
// never persists rule Statement bodies. A global boundary region selects the
// CLOUDFRONT scope on the us-east-1 endpoint; a concrete region selects
// REGIONAL.
package awssdk
