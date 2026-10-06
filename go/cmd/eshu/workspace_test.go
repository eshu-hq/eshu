// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
)

// TestWorkspaceIndexingCommandsNoLongerPostReindex pins #7620: `eshu workspace
// plan|sync|index` used to POST a {scope, path, action} body that
// /api/v0/admin/reindex accepted and ignored. They now fail with removal
// guidance and never call the API.
func TestWorkspaceIndexingCommandsNoLongerPostReindex(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ESHU_SERVICE_URL", server.URL)

	for name, run := range map[string]func(*cobra.Command, []string) error{
		"eshu workspace plan":  runWorkspacePlan,
		"eshu workspace sync":  runWorkspaceSync,
		"eshu workspace index": runWorkspaceIndex,
	} {
		err := run(&cobra.Command{}, []string{"/src/workspace"})
		if err == nil || !strings.Contains(err.Error(), name+" removed from supported Go CLI contract") {
			t.Fatalf("%s error = %v, want the removed-command error", name, err)
		}
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("API requests = %d, want 0", got)
	}
}
