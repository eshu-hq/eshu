// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/maintenance"
)

type stubReindexStateStore struct {
	state     runtimecfg.ReindexRequest
	err       error
	ingesters []string
}

func (s *stubReindexStateStore) GetReindexState(_ context.Context, ingester string) (runtimecfg.ReindexRequest, error) {
	s.ingesters = append(s.ingesters, ingester)
	return s.state, s.err
}

// TestReindexWatermarkReaderMapsRequestState pins the adapter between the
// runtime_ingester_control reindex request and the git collector watermark:
// it reads the repository ingester's row, returns the stored request time in
// UTC whatever the request status, and returns zero for no row or for the
// '0001-01-01' sentinel the state query substitutes for NULL, in any session
// time zone.
func TestReindexWatermarkReaderMapsRequestState(t *testing.T) {
	t.Parallel()

	requestedAt := time.Date(2026, 10, 5, 8, 0, 0, 0, time.FixedZone("EDT", -4*3600))
	sentinel := time.Date(1, 1, 1, 0, 0, 0, 0, time.FixedZone("EST", -5*3600))
	cases := []struct {
		name  string
		state runtimecfg.ReindexRequest
		want  time.Time
	}{
		{"pending request", runtimecfg.ReindexRequest{State: runtimecfg.RequestStatePending, RequestedAt: requestedAt}, requestedAt.UTC()},
		{"claimed by an older binary", runtimecfg.ReindexRequest{State: runtimecfg.RequestStateCompleted, RequestedAt: requestedAt}, requestedAt.UTC()},
		{"no row", runtimecfg.ReindexRequest{State: runtimecfg.RequestStateIdle}, time.Time{}},
		{"null sentinel", runtimecfg.ReindexRequest{State: runtimecfg.RequestStateIdle, RequestedAt: sentinel}, time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &stubReindexStateStore{state: tc.state}
			got, err := reindexWatermarkReader{store: store}.ReindexWatermark(context.Background())
			if err != nil {
				t.Fatalf("ReindexWatermark() error = %v", err)
			}
			if !got.Equal(tc.want) || got.Location() != time.UTC {
				t.Fatalf("ReindexWatermark() = %v, want %v in UTC", got, tc.want)
			}
			if len(store.ingesters) != 1 || store.ingesters[0] != runtimecfg.ReindexIngesterRepository {
				t.Fatalf("read ingesters %v, want [%s]", store.ingesters, runtimecfg.ReindexIngesterRepository)
			}
		})
	}

	failing := &stubReindexStateStore{err: errors.New("postgres down")}
	if _, err := (reindexWatermarkReader{store: failing}).ReindexWatermark(context.Background()); err == nil {
		t.Fatal("ReindexWatermark() error = nil, want the read failure")
	}
}

// TestBuildIngesterCollectorServiceWiresReindexWatermark fails when a git
// selector the ingester builds cannot see POST /api/v0/admin/reindex (#7620).
func TestBuildIngesterCollectorServiceWiresReindexWatermark(t *testing.T) {
	t.Parallel()

	build := func(env map[string]string) git.RepositorySelector {
		t.Helper()
		service, err := buildIngesterCollectorService(postgres.SQLDB{}, mapGetenv(env),
			func() (string, error) { return t.TempDir(), nil },
			func() []string { return []string{"PATH=/usr/bin"} }, nil, nil, nil)
		if err != nil {
			t.Fatalf("buildIngesterCollectorService() error = %v", err)
		}
		return service.Source.(*git.GitSource).Selector
	}
	requireReader := func(name string, reader git.ReindexWatermarkReader, repository git.RepositoryReindexWatermarkReader) {
		t.Helper()
		if _, ok := reader.(reindexWatermarkReader); !ok {
			t.Fatalf("%s ReindexWatermark = %T, want reindexWatermarkReader", name, reader)
		}
		if _, ok := repository.(maintenancestore.RepositoryReindexStore); !ok {
			t.Fatalf("%s RepositoryReindexWatermark = %T, want maintenancestore.RepositoryReindexStore", name, repository)
		}
	}

	native := build(map[string]string{}).(git.NativeRepositorySelector)
	requireReader("native selector", native.ReindexWatermark, native.RepositoryReindexWatermark)

	webhookOnly := build(map[string]string{
		"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED": "true",
		"ESHU_REPO_SCHEDULED_SYNC_ENABLED":     "false",
	}).(git.WebhookTriggerRepositorySelector)
	requireReader("webhook-only selector", webhookOnly.ReindexWatermark, webhookOnly.RepositoryReindexWatermark)

	priority := build(map[string]string{"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED": "true"}).(git.PriorityRepositorySelector)
	webhookSelector := priority.Selectors[0].(git.WebhookTriggerRepositorySelector)
	nativeSelector := priority.Selectors[1].(git.NativeRepositorySelector)
	requireReader("priority webhook selector", webhookSelector.ReindexWatermark, webhookSelector.RepositoryReindexWatermark)
	requireReader("priority native selector", nativeSelector.ReindexWatermark, nativeSelector.RepositoryReindexWatermark)
}
