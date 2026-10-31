// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"
)

func TestCoordinatorChecksRoleContractBeforePostgresDial(t *testing.T) {
	t.Setenv("ESHU_POSTGRES_ROLE_CONTRACT_JSON", `{"policy_revision":0}`)
	t.Setenv("ESHU_POSTGRES_DSN", "invalid-dsn")

	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "has invalid revision or database identity") {
		t.Fatalf("coordinator startup error = %v, want role contract validation before Postgres dial", err)
	}
}
