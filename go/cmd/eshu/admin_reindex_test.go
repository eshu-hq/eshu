// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestAdminReindexRepositoryFlag pins `eshu admin reindex`: with no
// --repository it posts the workspace body, and repeated --repository flags
// post a repository-scoped body without needing --scope (#7620).
func TestAdminReindexRepositoryFlag(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		bodies = append(bodies, body)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ESHU_SERVICE_URL", server.URL)

	cases := []struct {
		name string
		args []string
		want map[string]any
	}{
		{"workspace", nil, map[string]any{"ingester": "repository", "scope": "workspace", "force": true}},
		{
			"repositories",
			[]string{"--repository", "payments", "--repository", "orders"},
			map[string]any{"ingester": "repository", "scope": "repository", "force": true, "repositories": []any{"payments", "orders"}},
		},
		{
			"explicit workspace scope is sent as given",
			[]string{"--scope", "workspace", "--repository", "payments"},
			map[string]any{"ingester": "repository", "scope": "workspace", "force": true, "repositories": []any{"payments"}},
		},
	}
	for _, tc := range cases {
		cmd := newAdminReindexCmd()
		if err := cmd.ParseFlags(tc.args); err != nil {
			t.Fatalf("%s: ParseFlags() error = %v", tc.name, err)
		}
		if err := runAdminReindex(cmd, nil); err != nil {
			t.Fatalf("%s: runAdminReindex() error = %v", tc.name, err)
		}
		if got := bodies[len(bodies)-1]; !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: body = %#v, want %#v", tc.name, got, tc.want)
		}
	}
}
