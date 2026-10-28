// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contenttools

import (
	"testing"

	routecontract "github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

func TestPHPConfigurationRoute(t *testing.T) {
	t.Parallel()
	got, ok := Route("search_php_configuration", routecontract.Arguments{"mode": "mod_php", "repo_id": "repo-a", "limit": 12, "cursor": "page-token"})
	body, _ := got.Body.(map[string]any)
	if !ok || got.Method != "POST" || got.Path != "/api/v0/content/php-config/search" || body["mode"] != "mod_php" || body["repo_id"] != "repo-a" || body["limit"] != 12 || body["cursor"] != "page-token" {
		t.Fatalf("route=%+v ok=%t", got, ok)
	}
}
