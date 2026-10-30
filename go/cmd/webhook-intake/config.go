// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strconv"
	"strings"
)

// defaultMaxWebhookIntakeBodyBytes caps one intake request body at 1 MiB,
// matching the provider-facing webhook listener cap and the intake wire cap.
const defaultMaxWebhookIntakeBodyBytes = int64(1 << 20)

// webhookIntakeConfig carries the dedicated intake host's runtime settings.
// It holds no provider secrets and no provider route paths: this host serves
// core's authenticated intake API only and never faces providers directly.
type webhookIntakeConfig struct {
	// IntakeToken is the Bearer the satellite presents. Required: an
	// intake API without authentication must never serve. It arrives from
	// the process environment (populated from a secret object in
	// deployment) and is never logged.
	IntakeToken string
	// MaxRequestBodyBytes bounds one intake request body.
	MaxRequestBodyBytes int64
}

func loadWebhookIntakeConfig(getenv func(string) string) (webhookIntakeConfig, error) {
	if getenv == nil {
		return webhookIntakeConfig{}, fmt.Errorf("webhook intake getenv is required")
	}
	maxBody, err := intakeBodyBytesFromEnv(getenv)
	if err != nil {
		return webhookIntakeConfig{}, err
	}
	cfg := webhookIntakeConfig{
		IntakeToken:         strings.TrimSpace(getenv("ESHU_WEBHOOK_INTAKE_TOKEN")),
		MaxRequestBodyBytes: maxBody,
	}
	if cfg.IntakeToken == "" {
		return webhookIntakeConfig{}, fmt.Errorf("webhook intake token is required")
	}
	return cfg, nil
}

// intakeBodyBytesFromEnv resolves the intake body cap. Unset means the
// default; an explicitly set but non-positive or non-numeric value is a
// configuration error, never a silent fallback: a wrong cap changes the
// provider-visible 413 boundary. The error names the variable, never the
// Bearer.
func intakeBodyBytesFromEnv(getenv func(string) string) (int64, error) {
	raw := strings.TrimSpace(getenv("ESHU_WEBHOOK_MAX_BODY_BYTES"))
	if raw == "" {
		return defaultMaxWebhookIntakeBodyBytes, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("webhook intake max body bytes must be positive")
	}
	return value, nil
}
