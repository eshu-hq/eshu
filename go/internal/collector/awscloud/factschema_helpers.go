// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"strings"
	"time"
)

func stringValuePtr(value string) *string {
	return &value
}

func boolValuePtr(value bool) *bool {
	return &value
}

func intValuePtr(value int) *int {
	return &value
}

func int64ValuePtr(value int64) *int64 {
	return &value
}

func timeValuePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func boundaryValue(value string) *string {
	trimmed := strings.TrimSpace(value)
	return &trimmed
}

func awsPayloadAttributes(boundary Boundary, attributes map[string]any) map[string]any {
	return map[string]any{
		"service_kind":          strings.TrimSpace(boundary.ServiceKind),
		"collector_instance_id": boundary.CollectorInstanceID,
		"attributes":            cloneAnyMap(attributes),
	}
}
