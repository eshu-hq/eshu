// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"reflect"
	"testing"
)

// recordingClient captures the last POST and answers with an empty object.
type recordingClient struct {
	path string
	body any
}

func (c *recordingClient) Get(string, any) error { return nil }

func (c *recordingClient) Post(path string, body, _ any) error {
	c.path, c.body = path, body
	return nil
}

// TestReindexBody pins the request body: a workspace reindex sends exactly
// the three fields it always sent, and a repository reindex adds the
// repositories list (#7620).
func TestReindexBody(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   ReindexInput
		want map[string]any
	}{
		{
			"workspace",
			ReindexInput{Ingester: "repository", Scope: "workspace", Force: true},
			map[string]any{"ingester": "repository", "scope": "workspace", "force": true},
		},
		{
			"repository",
			ReindexInput{Ingester: "repository", Scope: "repository", Force: true, Repositories: []string{"payments", "orders"}},
			map[string]any{"ingester": "repository", "scope": "repository", "force": true, "repositories": []string{"payments", "orders"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := &recordingClient{}
			if _, err := Reindex(client, tc.in); err != nil {
				t.Fatalf("Reindex() error = %v", err)
			}
			if client.path != "/api/v0/admin/reindex" {
				t.Fatalf("path = %q, want /api/v0/admin/reindex", client.path)
			}
			if !reflect.DeepEqual(client.body, tc.want) {
				t.Fatalf("body = %#v, want %#v", client.body, tc.want)
			}
		})
	}
}
