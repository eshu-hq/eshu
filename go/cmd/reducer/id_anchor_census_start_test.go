// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph/anchor"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
)

// censusReader answers the census statement and counts reads.
type censusReader struct {
	reads  atomic.Int64
	cypher atomic.Value
}

func (r *censusReader) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (r *censusReader) RunSingle(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
	r.reads.Add(1)
	r.cypher.Store(cypher)
	return map[string]any{"id_bearing": int64(4), "via_id": int64(1), "via_uid_only": int64(3), "residual": int64(0)}, nil
}

func TestStartIDAnchorCensusRunsTheStatementOnNeo4jAndStopsOnCancel(t *testing.T) {
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	reader := &censusReader{}
	ctx, cancel := context.WithCancel(context.Background())
	cfg := idAnchorCensusConfig{Enabled: true, PollInterval: time.Hour, Timeout: time.Second}
	wait := startIDAnchorCensus(ctx, cfg, runtimecfg.GraphBackendNeo4j, reader, nil, logger)
	deadline := time.After(2 * time.Second)
	for reader.reads.Load() == 0 {
		select {
		case <-deadline:
			t.Fatal("the startup census pass did not run")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	done := make(chan struct{})
	go func() { wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown wait did not return after cancel")
	}
	if got, _ := reader.cypher.Load().(string); got != anchor.CensusCypher {
		t.Errorf("reader ran %q, want the census statement", got)
	}
	if !strings.Contains(logs.String(), "first_pass=true") {
		t.Errorf("startup pass was not logged:\n%s", logs.String())
	}
}

func TestStartIDAnchorCensusDoesNotReadWhenNotApplicable(t *testing.T) {
	cfg := idAnchorCensusConfig{Enabled: true, PollInterval: time.Hour, Timeout: time.Second}
	for name, tc := range map[string]struct {
		cfg     idAnchorCensusConfig
		backend runtimecfg.GraphBackend
	}{
		"nornicdb": {cfg, runtimecfg.GraphBackendNornicDB},
		"disabled": {idAnchorCensusConfig{}, runtimecfg.GraphBackendNeo4j},
	} {
		logs := &bytes.Buffer{}
		reader := &censusReader{}
		wait := startIDAnchorCensus(context.Background(), tc.cfg, tc.backend, reader, nil, slog.New(slog.NewTextHandler(logs, nil)))
		wait()
		time.Sleep(20 * time.Millisecond)
		if reader.reads.Load() != 0 {
			t.Errorf("%s: the census read the graph", name)
		}
		if !strings.Contains(logs.String(), "id anchor census not started") {
			t.Errorf("%s: no log line says why:\n%s", name, logs.String())
		}
	}
}

func TestIDAnchorCensusReaderPrefersTheRawRunnerOverTheCaptureDecorator(t *testing.T) {
	raw := &censusReader{}
	decorated := &censusReader{}
	got := idAnchorCensusReader(rawReaderAdapter{raw}, decorated)
	if _, err := got.RunSingle(context.Background(), "q", nil); err != nil {
		t.Fatal(err)
	}
	if raw.reads.Load() != 1 || decorated.reads.Load() != 0 {
		t.Fatalf("raw reads=%d decorated reads=%d, want the raw runner only (a decorated read would be recorded in the differential capture)", raw.reads.Load(), decorated.reads.Load())
	}
	if idAnchorCensusReader(nil, nil) != nil {
		t.Error("no reader configured must yield nil, not a typed-nil interface")
	}
}

// rawReaderAdapter lets a censusReader stand in for the raw session runner,
// which is both a CypherReader and a row reader.
type rawReaderAdapter struct{ *censusReader }

func (rawReaderAdapter) QueryCypherExists(context.Context, string, map[string]any) (bool, error) {
	return false, nil
}
