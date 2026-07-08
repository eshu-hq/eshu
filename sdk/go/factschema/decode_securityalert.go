// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	securityalertv1 "github.com/eshu-hq/eshu/sdk/go/factschema/securityalert/v1"
)

// DecodeSecurityAlertRepositoryAlert decodes env.Payload into the latest
// securityalertv1.RepositoryAlert struct for the
// "security_alert.repository_alert" fact kind, dispatching on
// env.SchemaVersion major per Contract System v1 §3.2. Callers (reducer
// handlers) receive either the decoded struct or a classified *DecodeError
// naming the missing required repository_id field; they must never substitute
// a zero-value struct on error.
func DecodeSecurityAlertRepositoryAlert(env Envelope) (securityalertv1.RepositoryAlert, error) {
	return decodeLatestMajor[securityalertv1.RepositoryAlert](FactKindSecurityAlertRepositoryAlert, env)
}

// EncodeSecurityAlertRepositoryAlert marshals a securityalertv1.RepositoryAlert
// into the map[string]any payload shape an Envelope carries. It is the inverse
// of DecodeSecurityAlertRepositoryAlert for schema-version-1 payloads, used by
// collectors emitting this fact kind and by this module's round-trip tests.
func EncodeSecurityAlertRepositoryAlert(alert securityalertv1.RepositoryAlert) (map[string]any, error) {
	return encodeDirectPayload(alert)
}
