// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/pagerduty"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type pagerDutyRuntimeConfiguration struct {
	Targets []targetJSON `json:"targets"`
}

type targetJSON struct {
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

func loadPagerDutySourceConfig(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (pagerduty.SourceConfig, error) {
	var decoded pagerDutyRuntimeConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &decoded); err != nil {
		return pagerduty.SourceConfig{}, fmt.Errorf("decode pagerduty collector configuration: %w", err)
	}
	targets := make([]pagerduty.TargetConfig, 0, len(decoded.Targets))
	for i, target := range decoded.Targets {
		mapped, err := mapTarget(target, getenv)
		if err != nil {
			return pagerduty.SourceConfig{}, fmt.Errorf("targets[%d]: %w", i, err)
		}
		targets = append(targets, mapped)
	}
	return pagerduty.SourceConfig{CollectorInstanceID: instance.InstanceID, Targets: targets}, nil
}

func mapTarget(target targetJSON, getenv func(string) string) (pagerduty.TargetConfig, error) {
	tokenEnv := strings.TrimSpace(target.TokenEnv)
	token := ""
	if tokenEnv != "" {
		token = strings.TrimSpace(getenv(tokenEnv))
	}
	if token == "" {
		return pagerduty.TargetConfig{}, fmt.Errorf("token_env %s did not resolve a credential", tokenEnv)
	}
	lookback := time.Duration(0)
	if raw := strings.TrimSpace(target.IncidentLookback); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return pagerduty.TargetConfig{}, fmt.Errorf("parse incident_lookback: %w", err)
		}
		lookback = parsed
	}
	return pagerduty.TargetConfig{
		Provider:                strings.TrimSpace(target.Provider),
		ScopeID:                 strings.TrimSpace(target.ScopeID),
		AccountID:               strings.TrimSpace(target.AccountID),
		Token:                   token,
		APIBaseURL:              strings.TrimRight(strings.TrimSpace(target.APIBaseURL), "/"),
		SourceURI:               strings.TrimSpace(firstNonBlank(target.SourceURI, target.APIBaseURL)),
		IncidentLimit:           target.IncidentLimit,
		IncidentLookback:        lookback,
		LogEntryLimit:           target.LogEntryLimit,
		ChangeEventLimit:        target.ChangeEventLimit,
		AllowedServiceIDs:       cleanConfigStrings(target.AllowedServiceIDs),
		ConfigValidationEnabled: target.ConfigValidationEnabled,
		ConfigResourceLimit:     target.ConfigResourceLimit,
	}, nil
}

func cleanConfigStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
