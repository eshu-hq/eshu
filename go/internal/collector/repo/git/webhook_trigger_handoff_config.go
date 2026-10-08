// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"strconv"
	"strings"
	"time"
)

const (
	webhookTriggerHandoffEnabledEnv   = "ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED"
	webhookTriggerHandoffOwnerEnv     = "ESHU_WEBHOOK_TRIGGER_HANDOFF_OWNER"
	webhookTriggerClaimLimitEnv       = "ESHU_WEBHOOK_TRIGGER_CLAIM_LIMIT"
	webhookTriggerClaimLeaseWindowEnv = "ESHU_WEBHOOK_TRIGGER_CLAIM_LEASE_WINDOW"
	webhookTriggerMaxClaimAttemptsEnv = "ESHU_WEBHOOK_TRIGGER_MAX_CLAIM_ATTEMPTS"
)

// WebhookTriggerHandoffConfig carries the shared env contract for collector
// compatibility handoff from durable webhook refresh triggers.
type WebhookTriggerHandoffConfig struct {
	Enabled    bool
	Owner      string
	ClaimLimit int
	// ClaimLeaseWindow overrides the stale-claim lease (#7661). Zero
	// keeps the selector default; a Go duration string such as "15m".
	ClaimLeaseWindow time.Duration
	// MaxClaimAttempts overrides the claim-attempt cap (#7661). Zero
	// keeps the selector default.
	MaxClaimAttempts int
}

// LoadWebhookTriggerHandoffConfig parses the shared webhook handoff env values
// used by collector-git and ingester.
func LoadWebhookTriggerHandoffConfig(defaultOwner string, getenv func(string) string) WebhookTriggerHandoffConfig {
	return WebhookTriggerHandoffConfig{
		Enabled:          parseWebhookTriggerHandoffEnabled(getenv),
		Owner:            parseWebhookTriggerHandoffOwner(defaultOwner, getenv),
		ClaimLimit:       parseWebhookTriggerClaimLimit(getenv),
		ClaimLeaseWindow: parseWebhookTriggerClaimLeaseWindow(getenv),
		MaxClaimAttempts: parseWebhookTriggerMaxClaimAttempts(getenv),
	}
}

func parseWebhookTriggerHandoffEnabled(getenv func(string) string) bool {
	value := strings.TrimSpace(strings.ToLower(getenv(webhookTriggerHandoffEnabledEnv)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func parseWebhookTriggerHandoffOwner(defaultOwner string, getenv func(string) string) string {
	if owner := strings.TrimSpace(getenv(webhookTriggerHandoffOwnerEnv)); owner != "" {
		return owner
	}
	return defaultOwner
}

func parseWebhookTriggerClaimLimit(getenv func(string) string) int {
	raw := strings.TrimSpace(getenv(webhookTriggerClaimLimitEnv))
	if raw == "" {
		return 0
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0
	}
	return limit
}

// parseWebhookTriggerClaimLeaseWindow parses the stale-claim lease window
// (#7661). Empty, unparseable, or non-positive values return zero so the
// selector default applies: a zero or negative window would reap live
// claims from the in-flight sync.
func parseWebhookTriggerClaimLeaseWindow(getenv func(string) string) time.Duration {
	raw := strings.TrimSpace(getenv(webhookTriggerClaimLeaseWindowEnv))
	if raw == "" {
		return 0
	}
	window, err := time.ParseDuration(raw)
	if err != nil || window <= 0 {
		return 0
	}
	return window
}

// parseWebhookTriggerMaxClaimAttempts parses the claim-attempt cap (#7661).
// Empty, unparseable, or sub-one values return zero so the selector
// default applies: a cap below 1 would fail every row on its first reap.
func parseWebhookTriggerMaxClaimAttempts(getenv func(string) string) int {
	raw := strings.TrimSpace(getenv(webhookTriggerMaxClaimAttemptsEnv))
	if raw == "" {
		return 0
	}
	attempts, err := strconv.Atoi(raw)
	if err != nil || attempts <= 0 {
		return 0
	}
	return attempts
}
