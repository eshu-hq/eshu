// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Warning is the schema-version-1 typed payload for "aws_warning".
type Warning struct {
	AccountID           string         `json:"account_id"`
	Region              string         `json:"region"`
	ServiceKind         *string        `json:"service_kind,omitempty"`
	CollectorInstanceID *string        `json:"collector_instance_id,omitempty"`
	WarningKind         string         `json:"warning_kind"`
	ErrorClass          *string        `json:"error_class,omitempty"`
	Message             *string        `json:"message,omitempty"`
	Attributes          map[string]any `json:"attributes,omitempty"`
}
