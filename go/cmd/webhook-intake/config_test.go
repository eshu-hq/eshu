// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

func testIntakeEnv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

// The intake host fails closed without a Bearer: an intake API without
// authentication must never serve.
func TestLoadWebhookIntakeConfigRequiresToken(t *testing.T) {
	t.Parallel()

	for name, values := range map[string]map[string]string{
		"missing": {},
		"empty":   {"ESHU_WEBHOOK_INTAKE_TOKEN": ""},
		"blank":   {"ESHU_WEBHOOK_INTAKE_TOKEN": "   "},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := loadWebhookIntakeConfig(testIntakeEnv(values)); err == nil {
				t.Fatal("loadWebhookIntakeConfig() error = nil, want fail-closed error")
			}
		})
	}
}

func TestLoadWebhookIntakeConfigDefaultsBodyCap(t *testing.T) {
	t.Parallel()

	cfg, err := loadWebhookIntakeConfig(testIntakeEnv(map[string]string{
		"ESHU_WEBHOOK_INTAKE_TOKEN": "serve-token",
	}))
	if err != nil {
		t.Fatalf("loadWebhookIntakeConfig() error = %v, want nil", err)
	}
	if cfg.IntakeToken != "serve-token" {
		t.Fatalf("IntakeToken = %q, want %q", cfg.IntakeToken, "serve-token")
	}
	if cfg.MaxRequestBodyBytes != defaultMaxWebhookIntakeBodyBytes {
		t.Fatalf("MaxRequestBodyBytes = %d, want %d", cfg.MaxRequestBodyBytes, int64(defaultMaxWebhookIntakeBodyBytes))
	}
}

func TestLoadWebhookIntakeConfigHonorsBodyCap(t *testing.T) {
	t.Parallel()

	cfg, err := loadWebhookIntakeConfig(testIntakeEnv(map[string]string{
		"ESHU_WEBHOOK_INTAKE_TOKEN":   "serve-token",
		"ESHU_WEBHOOK_MAX_BODY_BYTES": "2048",
	}))
	if err != nil {
		t.Fatalf("loadWebhookIntakeConfig() error = %v, want nil", err)
	}
	if cfg.MaxRequestBodyBytes != 2048 {
		t.Fatalf("MaxRequestBodyBytes = %d, want 2048", cfg.MaxRequestBodyBytes)
	}
}

func TestLoadWebhookIntakeConfigRejectsNonPositiveBodyCap(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"0", "-10", "not-a-number"} {
		if _, err := loadWebhookIntakeConfig(testIntakeEnv(map[string]string{
			"ESHU_WEBHOOK_INTAKE_TOKEN":   "serve-token",
			"ESHU_WEBHOOK_MAX_BODY_BYTES": raw,
		})); err == nil {
			t.Fatalf("loadWebhookIntakeConfig(body=%q) error = nil, want positive-bytes error", raw)
		}
	}
}

// The Bearer value must never appear in an error: config failures are
// operator-visible and the token lives in a secret only.
func TestLoadWebhookIntakeConfigNeverLeaksToken(t *testing.T) {
	t.Parallel()

	const token = "super-secret-intake-token"
	_, err := loadWebhookIntakeConfig(testIntakeEnv(map[string]string{
		"ESHU_WEBHOOK_INTAKE_TOKEN":   token,
		"ESHU_WEBHOOK_MAX_BODY_BYTES": "0",
	}))
	if err == nil {
		t.Fatal("loadWebhookIntakeConfig() error = nil, want body-cap error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error %q contains the Bearer, want redacted", err.Error())
	}
}
