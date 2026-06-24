// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const maxPagerDutyPageLimit = 100

type pagerDutyCollectorConfiguration struct {
	Targets []pagerDutyTargetConfiguration `json:"targets"`
}

type pagerDutyTargetConfiguration struct {
	Provider                string   `json:"provider"`
	ScopeID                 string   `json:"scope_id"`
	AccountID               string   `json:"account_id"`
	TokenEnv                string   `json:"token_env"`
	APIBaseURL              string   `json:"api_base_url"`
	SourceURI               string   `json:"source_uri"`
	IncidentLimit           int      `json:"incident_limit"`
	IncidentLookback        string   `json:"incident_lookback"`
	LogEntryLimit           int      `json:"log_entry_limit"`
	ChangeEventLimit        int      `json:"change_event_limit"`
	AllowedServiceIDs       []string `json:"allowed_service_ids"`
	ConfigValidationEnabled bool     `json:"config_validation_enabled"`
	ConfigResourceLimit     int      `json:"config_resource_limit"`
}

// ValidatePagerDutyCollectorConfiguration checks bounded PagerDuty collector
// targets without resolving credentials or contacting PagerDuty.
func ValidatePagerDutyCollectorConfiguration(raw string) error {
	var decoded pagerDutyCollectorConfiguration
	if err := json.Unmarshal([]byte(normalizeJSONDocument(raw)), &decoded); err != nil {
		return fmt.Errorf("decode pagerduty collector configuration: %w", err)
	}
	if len(decoded.Targets) == 0 {
		return fmt.Errorf("pagerduty collector configuration requires targets")
	}
	seen := make(map[string]struct{}, len(decoded.Targets))
	for i, target := range decoded.Targets {
		if err := validatePagerDutyTargetConfiguration(target); err != nil {
			return fmt.Errorf("targets[%d]: %w", i, err)
		}
		scopeID := strings.TrimSpace(target.ScopeID)
		if _, ok := seen[scopeID]; ok {
			return fmt.Errorf("duplicate pagerduty target scope_id %q", scopeID)
		}
		seen[scopeID] = struct{}{}
	}
	return nil
}

func validatePagerDutyTargetConfiguration(target pagerDutyTargetConfiguration) error {
	if strings.TrimSpace(target.ScopeID) == "" {
		return fmt.Errorf("scope_id is required")
	}
	if strings.TrimSpace(target.Provider) != "pagerduty" {
		if strings.TrimSpace(target.Provider) == "" {
			return fmt.Errorf("provider is required")
		}
		return fmt.Errorf("unsupported pagerduty provider %q", strings.TrimSpace(target.Provider))
	}
	if strings.TrimSpace(target.AccountID) == "" {
		return fmt.Errorf("account_id is required")
	}
	if strings.TrimSpace(target.TokenEnv) == "" {
		return fmt.Errorf("token_env is required")
	}
	if target.IncidentLimit < 0 || target.IncidentLimit > maxPagerDutyPageLimit {
		return fmt.Errorf("incident_limit must be between 0 and %d", maxPagerDutyPageLimit)
	}
	if target.LogEntryLimit < 0 || target.LogEntryLimit > maxPagerDutyPageLimit {
		return fmt.Errorf("log_entry_limit must be between 0 and %d", maxPagerDutyPageLimit)
	}
	if target.ChangeEventLimit < 0 || target.ChangeEventLimit > maxPagerDutyPageLimit {
		return fmt.Errorf("change_event_limit must be between 0 and %d", maxPagerDutyPageLimit)
	}
	if target.ConfigResourceLimit < 0 || target.ConfigResourceLimit > maxPagerDutyPageLimit {
		return fmt.Errorf("config_resource_limit must be between 0 and %d", maxPagerDutyPageLimit)
	}
	if strings.TrimSpace(target.IncidentLookback) != "" {
		value, err := time.ParseDuration(strings.TrimSpace(target.IncidentLookback))
		if err != nil {
			return fmt.Errorf("parse incident_lookback: %w", err)
		}
		if value <= 0 {
			return fmt.Errorf("incident_lookback must be positive")
		}
	}
	if err := validatePagerDutyURL("api_base_url", target.APIBaseURL, true); err != nil {
		return err
	}
	return validatePagerDutyURL("source_uri", target.SourceURI, false)
}

func validatePagerDutyURL(field, raw string, requireHTTPS bool) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("parse %s: %w", field, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must include scheme and host", field)
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use https", field)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%s must use http or https", field)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not include credentials", field)
	}
	return nil
}
