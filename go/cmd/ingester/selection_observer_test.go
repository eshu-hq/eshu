// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildIngesterCollectorServiceWiresSelectionObserver fails when a git
// selector the ingester builds cannot record the #7625 selection
// evaluation. The webhook selector stays unwired: the webhook path never
// evaluates.
func TestBuildIngesterCollectorServiceWiresSelectionObserver(t *testing.T) {
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

	native := build(map[string]string{}).(git.NativeRepositorySelector)
	if _, ok := native.SelectionObserver.(postgres.SelectionObservationStore); !ok {
		t.Fatalf("native SelectionObserver = %T, want postgres.SelectionObservationStore", native.SelectionObserver)
	}

	priority := build(map[string]string{"ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED": "true"}).(git.PriorityRepositorySelector)
	inner := priority.Selectors[1].(git.NativeRepositorySelector)
	if _, ok := inner.SelectionObserver.(postgres.SelectionObservationStore); !ok {
		t.Fatalf("priority native SelectionObserver = %T, want postgres.SelectionObservationStore", inner.SelectionObserver)
	}
}
