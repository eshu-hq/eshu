// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "github.com/eshu-hq/eshu/go/internal/telemetry/contract/thirdparty"

// Compat surface for go/internal/telemetry/contract/thirdparty (issue #6777).
// The hosted third-party-source span, attribute, and dimension declarations
// that used to live directly in this package as contract_jira.go,
// contract_pagerduty.go, and contract_vaultlive.go now live in the thirdparty
// subpackage; every name below re-exports one of them as a root telemetry.*
// identifier so every existing caller keeps compiling unchanged. See the
// referenced thirdparty.* symbol for the canonical doc comment.

// Jira source-collection spans and bounded fetch-span attributes (from
// contract/thirdparty/jira.go).
const (
	SpanJiraObserve = thirdparty.SpanJiraObserve
	SpanJiraFetch   = thirdparty.SpanJiraFetch

	SpanAttrJiraSearchPages              = thirdparty.SpanAttrJiraSearchPages
	SpanAttrJiraChangelogPages           = thirdparty.SpanAttrJiraChangelogPages
	SpanAttrJiraRemoteLinkPages          = thirdparty.SpanAttrJiraRemoteLinkPages
	SpanAttrJiraIssuesEmitted            = thirdparty.SpanAttrJiraIssuesEmitted
	SpanAttrJiraChangelogEventsEmitted   = thirdparty.SpanAttrJiraChangelogEventsEmitted
	SpanAttrJiraRemoteLinksEmitted       = thirdparty.SpanAttrJiraRemoteLinksEmitted
	SpanAttrJiraRemoteLinksRejected      = thirdparty.SpanAttrJiraRemoteLinksRejected
	SpanAttrJiraUnsupportedProviderLinks = thirdparty.SpanAttrJiraUnsupportedProviderLinks
	SpanAttrJiraMetadataPages            = thirdparty.SpanAttrJiraMetadataPages
	SpanAttrJiraMetadataObjectsScanned   = thirdparty.SpanAttrJiraMetadataObjectsScanned
	SpanAttrJiraMetadataObjectsEmitted   = thirdparty.SpanAttrJiraMetadataObjectsEmitted
	SpanAttrJiraUnsupportedMetadata      = thirdparty.SpanAttrJiraUnsupportedMetadata
	SpanAttrJiraPermissionHiddenMetadata = thirdparty.SpanAttrJiraPermissionHiddenMetadata
	SpanAttrJiraStaleMetadata            = thirdparty.SpanAttrJiraStaleMetadata
	SpanAttrJiraMetadataRedactions       = thirdparty.SpanAttrJiraMetadataRedactions
	SpanAttrJiraPartialFailures          = thirdparty.SpanAttrJiraPartialFailures
	SpanAttrJiraRateLimits               = thirdparty.SpanAttrJiraRateLimits
	SpanAttrJiraRetryAfterSeconds        = thirdparty.SpanAttrJiraRetryAfterSeconds
	SpanAttrJiraStaleWindows             = thirdparty.SpanAttrJiraStaleWindows
)

// PagerDuty source-collection spans (from contract/thirdparty/pagerduty.go).
const (
	SpanPagerDutyObserve = thirdparty.SpanPagerDutyObserve
	SpanPagerDutyFetch   = thirdparty.SpanPagerDutyFetch
)

// Live Vault collector redaction dimension and bounded field_class values
// (from contract/thirdparty/vaultlive.go).
const (
	MetricDimensionFieldClass = thirdparty.MetricDimensionFieldClass

	FieldClassURIUserinfo = thirdparty.FieldClassURIUserinfo
	FieldClassURIQuery    = thirdparty.FieldClassURIQuery
	FieldClassURIFragment = thirdparty.FieldClassURIFragment
)
