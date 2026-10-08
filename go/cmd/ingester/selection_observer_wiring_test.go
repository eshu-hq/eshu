// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	membershipstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/membership"
)

// TestBuildIngesterCollectorServiceWiresSelectionObserver fails when the
// ingester's native githubOrg selector cannot record repository selection
// observations (#7625), standalone or behind the webhook priority selector.
func TestBuildIngesterCollectorServiceWiresSelectionObserver(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	build := func(env map[string]string) git.RepositorySelector {
		t.Helper()
		service, err := buildIngesterCollectorService(postgres.SQLDB{}, mapGetenv(env),
			func() (string, error) { return t.TempDir(), nil },
			func() []string { return []string{"PATH=/usr/bin"} }, nil, nil, logger)
		if err != nil {
			t.Fatalf("buildIngesterCollectorService() error = %v", err)
		}
		return service.Source.(*git.GitSource).Selector
	}
	requireObserver := func(name string, observer git.RepositorySelectionObserver) {
		t.Helper()
		wired, ok := observer.(membership.Observer)
		if !ok {
			t.Fatalf("%s SelectionObserver = %T, want membership.Observer", name, observer)
		}
		if _, ok := wired.Store.(membershipstore.ObservationStore); !ok {
			t.Fatalf("%s SelectionObserver.Store = %T, want membershipstore.ObservationStore", name, wired.Store)
		}
		if wired.Logger != logger {
			t.Fatalf("%s SelectionObserver.Logger is not the service logger", name)
		}
	}

	requireObserver("native selector", build(map[string]string{}).(git.NativeRepositorySelector).SelectionObserver)
	priority := build(map[string]string{"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED": "true"}).(git.PriorityRepositorySelector)
	requireObserver("priority native selector", priority.Selectors[1].(git.NativeRepositorySelector).SelectionObserver)
}
