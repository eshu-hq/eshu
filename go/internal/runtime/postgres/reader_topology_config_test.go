// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestLoadConfigExplicitPrimaryAllowsDistinctReaderCredentials(t *testing.T) {
	env := map[string]string{
		"ESHU_POSTGRES_DSN":           "postgres://writer:secret@same-primary/db",
		"ESHU_POSTGRES_READ_DSN":      "postgres://reader:secret@same-primary/db",
		"ESHU_POSTGRES_READ_TOPOLOGY": "primary",
	}
	cfg, err := LoadConfig(func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SamePrimary || !cfg.ExplicitReaderTopology {
		t.Fatalf("reader topology = same primary %t, explicit %t; want both true", cfg.SamePrimary, cfg.ExplicitReaderTopology)
	}
}

func TestLoadConfigRejectsUnknownReaderTopology(t *testing.T) {
	env := map[string]string{
		"ESHU_POSTGRES_DSN":           "postgres://writer:secret@same-primary/db",
		"ESHU_POSTGRES_READ_TOPOLOGY": "unknown",
	}
	_, err := LoadConfig(func(key string) string { return env[key] })
	if err == nil || !strings.Contains(err.Error(), "ESHU_POSTGRES_READ_TOPOLOGY") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("LoadConfig() error = %v, want sanitized topology error", err)
	}
}
