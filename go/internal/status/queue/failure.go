// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queue

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// FailureSnapshot captures the newest queued work failure metadata shown
// on operator status surfaces. Values are rendered only in status payloads and
// must not be promoted to metric labels.
type FailureSnapshot struct {
	Stage          string
	Domain         string
	Status         string
	WorkItemID     string
	ScopeID        string
	GenerationID   string
	FailureClass   string
	FailureMessage string
	FailureDetails string
	UpdatedAt      time.Time
}

const failureTextLimit = 240

func CloneFailure(snapshot *FailureSnapshot) *FailureSnapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	return &cloned
}

func FailureText(snapshot *FailureSnapshot) string {
	if snapshot == nil {
		return ""
	}

	parts := []string{
		fmt.Sprintf("stage=%s", snapshot.Stage),
		fmt.Sprintf("domain=%s", snapshot.Domain),
		fmt.Sprintf("status=%s", snapshot.Status),
		fmt.Sprintf("class=%s", snapshot.FailureClass),
	}
	if message := boundedFailureText(snapshot.FailureMessage); message != "" {
		parts = append(parts, fmt.Sprintf("message=%s", strconv.Quote(message)))
	}
	if details := boundedFailureText(snapshot.FailureDetails); details != "" {
		parts = append(parts, fmt.Sprintf("details=%s", strconv.Quote(details)))
	}

	return strings.Join(parts, " ")
}

func boundedFailureText(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= failureTextLimit {
		return value
	}
	return value[:failureTextLimit] + "..."
}
