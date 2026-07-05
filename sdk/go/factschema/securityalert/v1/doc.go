// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload struct for the
// "security_alert" fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_securityalert.go).
//
// One fact kind lives here:
//
//   - RepositoryAlert (security_alert.repository_alert): one repository-scoped
//     security alert reported by an external provider (for example GitHub
//     Dependabot). It is reconciled provider evidence only.
//
// This family has a deliberate accuracy boundary. The single decode site on
// the reducer side (extractProviderSecurityAlerts,
// go/internal/reducer/security_alert_reconciliation.go) feeds TWO consumers:
// the security-alert reconciliation read surface
// (GET /api/v0/supply-chain/security-alerts/reconciliations, the
// security_alert_reconciliation domain) and the supply-chain-impact seeder
// (appendSecurityAlertImpactFindings, the supply_chain_impact domain). The
// typed struct mirrors the existing wire payload EXACTLY — it does not add,
// rename, narrow, or widen any field — so it cannot change what flows into
// supply-chain-impact truth. The only behavior change is that a
// repository_alert missing its required RepositoryID now dead-letters as a
// per-fact input_invalid quarantine on BOTH consumers instead of silently
// producing a blank-repository reconciliation row or an empty-identity impact
// finding.
package v1
