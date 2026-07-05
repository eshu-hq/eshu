// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	securityalertv1 "github.com/eshu-hq/eshu/sdk/go/factschema/securityalert/v1"
)

// decodeSecurityAlertRepositoryAlert decodes one security_alert.repository_alert
// envelope into the typed securityalertv1.RepositoryAlert struct through the
// contracts seam, returning a self-classifying *factDecodeError when the
// payload is missing its required repository_id field or is otherwise
// malformed.
//
// It is the single decode site for the security_alert.repository_alert kind on
// the reducer side. Both consumers of this fact kind decode through here:
// extractProviderSecurityAlerts (feeding the security-alert reconciliation read
// surface via BuildSecurityAlertReconciliations, and the supply-chain-impact
// seeder via appendSecurityAlertImpactFindings). A missing required field is
// routed through partitionDecodeFailures so it dead-letters as a per-fact
// input_invalid quarantine rather than a silent blank-repository reconciliation
// row or an empty-identity impact finding — on both consumers.
func decodeSecurityAlertRepositoryAlert(env facts.Envelope) (securityalertv1.RepositoryAlert, error) {
	alert, err := factschema.DecodeSecurityAlertRepositoryAlert(factschemaEnvelope(env))
	if err != nil {
		return securityalertv1.RepositoryAlert{}, newFactDecodeError(factschema.FactKindSecurityAlertRepositoryAlert, err)
	}
	return alert, nil
}
