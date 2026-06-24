// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// SecurityAlertReconciliationStatus names how one provider alert compares to
// Eshu-owned dependency and impact evidence.
type SecurityAlertReconciliationStatus string

const (
	// SecurityAlertReconciliationMatched means provider alert, owned
	// dependency evidence, and reducer-owned impact evidence agree.
	SecurityAlertReconciliationMatched SecurityAlertReconciliationStatus = "matched"
	// SecurityAlertReconciliationUnmatched means Eshu sees the dependency but
	// has not admitted matching impact evidence for the provider advisory IDs.
	SecurityAlertReconciliationUnmatched SecurityAlertReconciliationStatus = "unmatched"
	// SecurityAlertReconciliationStale means newer owned dependency evidence no
	// longer matches the provider alert's manifest path.
	SecurityAlertReconciliationStale SecurityAlertReconciliationStatus = "stale"
	// SecurityAlertReconciliationDismissed means the provider alert is
	// dismissed or auto-dismissed at the source.
	SecurityAlertReconciliationDismissed SecurityAlertReconciliationStatus = "dismissed"
	// SecurityAlertReconciliationFixed means the provider alert is fixed at the
	// source.
	SecurityAlertReconciliationFixed SecurityAlertReconciliationStatus = "fixed"
	// SecurityAlertReconciliationProviderOnly means the alert has no matching
	// owned dependency evidence in the active Eshu fact set.
	SecurityAlertReconciliationProviderOnly SecurityAlertReconciliationStatus = "provider_only"
	// SecurityAlertReconciliationUnsupported means the provider alert names an
	// ecosystem or target shape Eshu cannot currently match.
	SecurityAlertReconciliationUnsupported SecurityAlertReconciliationStatus = "unsupported"
	// SecurityAlertReconciliationAmbiguous means multiple owned evidence
	// candidates could match the provider alert and the reducer refused to guess.
	SecurityAlertReconciliationAmbiguous SecurityAlertReconciliationStatus = "ambiguous"
)
