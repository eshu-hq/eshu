// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestBuildIngesterCollectorServiceEnablesEmptyBatchEscapeForSingleShardCollectorOff(t *testing.T) {
	t.Parallel()

	service, err := buildIngesterCollectorService(
		postgres.SQLDB{},
		mapGetenv(map[string]string{
			"ESHU_REPO_SHARD_COUNT":                "1",
			"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED": "true",
			"ESHU_REPO_SCHEDULED_SYNC_ENABLED":     "false",
		}),
		func() (string, error) { return t.TempDir(), nil },
		func() []string { return []string{"PATH=/usr/bin"} },
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildIngesterCollectorService() error = %v, want nil", err)
	}
	if !service.AfterEmptyBatchDrained {
		t.Fatal("AfterEmptyBatchDrained = false, want true for single-shard collector-off ingester (#7665: nothing else ever triggers the deferred relationship maintenance pass)")
	}
}

func TestBuildIngesterCollectorServiceKeepsEmptyBatchEscapeOffForSingleShardScheduledSync(t *testing.T) {
	t.Parallel()

	service, err := buildIngesterCollectorService(
		postgres.SQLDB{},
		mapGetenv(map[string]string{
			"ESHU_REPO_SHARD_COUNT":            "1",
			"ESHU_REPO_SCHEDULED_SYNC_ENABLED": "true",
		}),
		func() (string, error) { return t.TempDir(), nil },
		func() []string { return []string{"PATH=/usr/bin"} },
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildIngesterCollectorService() error = %v, want nil", err)
	}
	if service.AfterEmptyBatchDrained {
		t.Fatal("AfterEmptyBatchDrained = true, want false for single-shard ingester with scheduled sync on (#7665 keeps the normal case unchanged: commits trigger the pass)")
	}
}
