// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"fmt"
	"strings"
)

// AcknowledgeTerminalFailureInput names the failure to acknowledge and why.
// All four fields are mandatory.
type AcknowledgeTerminalFailureInput struct {
	// InstanceID is the collector instance, as collector readiness reports it.
	InstanceID string
	// ScopeID is the failed scope: last_failure_scope_id on collector status.
	ScopeID string
	// FailureClass is the last_failure_class read from collector readiness.
	FailureClass string
	// Reason says why the failure is known and expected.
	Reason string
}

// AcknowledgeTerminalFailure marks one collector terminal failure as known and
// expected (#8830) and returns the decoded response body. It refuses a blank
// field before any HTTP call. The messages name the CLI flag rather than the
// struct field, because they reach an operator running
// `eshu admin collector acknowledge-failure`.
func AcknowledgeTerminalFailure(client Client, in AcknowledgeTerminalFailureInput) (any, error) {
	for _, field := range []struct{ flag, value string }{
		{"--instance", in.InstanceID},
		{"--scope", in.ScopeID},
		{"--failure-class", in.FailureClass},
		{"--reason", in.Reason},
	} {
		if strings.TrimSpace(field.value) == "" {
			return nil, fmt.Errorf("%s is required", field.flag)
		}
	}
	body := map[string]any{
		"instance_id":   in.InstanceID,
		"scope_id":      in.ScopeID,
		"failure_class": in.FailureClass,
		"reason":        in.Reason,
	}
	var result any
	if err := client.Post("/api/v0/admin/collector-terminal-failure-acknowledgements", body, &result); err != nil {
		return nil, err //nolint:wrapcheck // Client's error is already the operator's message; cmd/eshu prints it verbatim and wrapping would prepend a second prefix.
	}
	return result, nil
}
