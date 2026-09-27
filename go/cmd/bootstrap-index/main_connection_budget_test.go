// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunRejectsSinglePostgresConnectionBeforeOpeningDatabase(t *testing.T) {
	t.Parallel()

	opened := false
	err := run(
		context.Background(),
		func(key string) string {
			if key == "ESHU_POSTGRES_MAX_OPEN_CONNS" {
				return "1"
			}
			return ""
		},
		func(context.Context, func(string) string) (bootstrapDB, error) {
			opened = true
			return nil, errors.New("database opened before pool budget validation")
		},
		nil, nil, noopSecretLines(), nil, nil, nil, nil,
	)
	if opened {
		t.Fatal("run opened the database with a single connection")
	}
	if err == nil || !strings.Contains(err.Error(), "ESHU_POSTGRES_MAX_OPEN_CONNS") {
		t.Fatalf("run error = %v, want a connection-budget error", err)
	}
}
