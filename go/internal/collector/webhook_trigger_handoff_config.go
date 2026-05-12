package collector

import (
	"strconv"
	"strings"
)

const (
	webhookTriggerHandoffEnabledEnv = "ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED"
	webhookTriggerHandoffOwnerEnv   = "ESHU_WEBHOOK_TRIGGER_HANDOFF_OWNER"
	webhookTriggerClaimLimitEnv     = "ESHU_WEBHOOK_TRIGGER_CLAIM_LIMIT"
)

// WebhookTriggerHandoffConfig carries the shared env contract for collector
// compatibility handoff from durable webhook refresh triggers.
type WebhookTriggerHandoffConfig struct {
	Enabled    bool
	Owner      string
	ClaimLimit int
}

// LoadWebhookTriggerHandoffConfig parses the shared webhook handoff env values
// used by collector-git and ingester.
func LoadWebhookTriggerHandoffConfig(defaultOwner string, getenv func(string) string) WebhookTriggerHandoffConfig {
	return WebhookTriggerHandoffConfig{
		Enabled:    parseWebhookTriggerHandoffEnabled(getenv),
		Owner:      parseWebhookTriggerHandoffOwner(defaultOwner, getenv),
		ClaimLimit: parseWebhookTriggerClaimLimit(getenv),
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
