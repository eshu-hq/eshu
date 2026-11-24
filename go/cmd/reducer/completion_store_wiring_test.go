// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"io"
	"log/slog"
	"testing"
)

// TestNewCrossScopeCompletionStoreCarriesTheServiceLogger pins that the
// reducer's completion store receives the service logger, so the warning Fanout
// writes when its post-commit manifest-marker count fails (#10972) reaches the
// operator's log stream instead of the process default.
func TestNewCrossScopeCompletionStoreCarriesTheServiceLogger(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := newCrossScopeCompletionStore(&fakeReducerDB{}, logger)
	if store == nil || store.Logger != logger {
		t.Fatalf("store = %+v, want the service logger attached", store)
	}
}
