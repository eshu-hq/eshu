// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestWireMCPRejectsInvalidRoleContractBeforeDatastore(t *testing.T) {
	env := map[string]string{
		"ESHU_POSTGRES_DSN":                "postgres://writer:secret@localhost:1/db",
		"ESHU_POSTGRES_READ_DSN":           "postgres://reader:secret@localhost:1/db",
		"ESHU_POSTGRES_READ_TOPOLOGY":      "primary",
		"ESHU_POSTGRES_ROLE_CONTRACT_JSON": "{",
		"ESHU_QUERY_PROFILE":               "local_lightweight",
		"ESHU_AUTH_BOOTSTRAP_MODE":         "disabled",
		"ESHU_API_KEY":                     "test-key",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, _, _, err := wireAPI(ctx, func(key string) string { return env[key] }, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "ESHU_POSTGRES_ROLE_CONTRACT_JSON is invalid JSON") {
		t.Fatalf("wireAPI() error = %v, want role contract error before datastore", err)
	}
}
