// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package thirdparty holds the hosted third-party-source span, span
// attribute, and metric-dimension declarations for Eshu's Go data-plane
// telemetry contract: Jira work-item evidence, PagerDuty incident
// evidence, and the live Vault collector's redaction dimension. These used
// to live directly in root package telemetry as contract_jira.go,
// contract_pagerduty.go, and contract_vaultlive.go (issue #6777).
//
// Like its sibling contract package, this package holds only declarations:
// no func init(), no registration, and no import of root package telemetry
// or any other go/internal/* package. Root registration.go registers every
// span here into the ordered spanNames slice, and the vaultlive
// metric-dimension into metricDimensionKeys; root compat_thirdparty.go
// re-exports every name as a root telemetry.* compat alias.
package thirdparty
