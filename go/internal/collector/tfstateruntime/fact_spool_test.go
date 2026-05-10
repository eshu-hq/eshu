package tfstateruntime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestFactSpoolReportsReplayDecodeErrors(t *testing.T) {
	t.Parallel()

	spool, err := newFactSpool()
	if err != nil {
		t.Fatalf("newFactSpool() error = %v, want nil", err)
	}
	if err := spool.Emit(context.Background(), facts.Envelope{FactID: "fact-1"}); err != nil {
		t.Fatalf("Emit() error = %v, want nil", err)
	}
	if err := spool.file.Truncate(1); err != nil {
		t.Fatalf("Truncate() error = %v, want nil", err)
	}

	stream, streamErr := spool.Stream(context.Background())
	for range stream {
	}

	err = streamErr()
	if err == nil {
		t.Fatal("streamErr() error = nil, want decode error")
	}
	if !strings.Contains(err.Error(), "read terraform state runtime fact spool") {
		t.Fatalf("streamErr() error = %q, want spool read context", err)
	}
}

func TestFactSpoolEmitStopsOnCanceledContext(t *testing.T) {
	t.Parallel()

	spool, err := newFactSpool()
	if err != nil {
		t.Fatalf("newFactSpool() error = %v, want nil", err)
	}
	defer spool.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = spool.Emit(ctx, facts.Envelope{FactID: "fact-1"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Emit() error = %v, want %v", err, context.Canceled)
	}
	if got, want := spool.count, 0; got != want {
		t.Fatalf("spool.count = %d, want %d", got, want)
	}
}
