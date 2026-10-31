// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"
)

func TestRunLoadsScannerWorkerPostgresRoleContract(t *testing.T) {
	t.Setenv("ESHU_POSTGRES_ROLE_CONTRACT_JSON", "{")
	err := run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ESHU_POSTGRES_ROLE_CONTRACT_JSON is invalid JSON") {
		t.Fatalf("run() error = %v, want role contract validation", err)
	}
}
