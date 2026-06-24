// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestBuildIngesterCollectorServiceWiresLoggerIntoRepositorySelector(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	service, err := buildIngesterCollectorService(
		postgres.SQLDB{},
		func(string) string { return "" },
		func() (string, error) { return t.TempDir(), nil },
		func() []string { return []string{"PATH=/usr/bin"} },
		nil,
		nil,
		logger,
	)
	if err != nil {
		t.Fatalf("buildIngesterCollectorService() error = %v, want nil", err)
	}

	source := service.Source.(*collector.GitSource)
	selector := source.Selector.(collector.NativeRepositorySelector)
	if selector.Logger == nil {
		t.Fatal("repository selector logger = nil, want non-nil")
	}
}
