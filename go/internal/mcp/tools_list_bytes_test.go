// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

// toolsListFullSHA256 pins the exact bytes the default (full) surface returns
// for tools/list. The MCP router epic (#8752) adds opt-in surfaces; the default
// must stay byte-identical. Update this value only for an approved change to
// the registered tool set, descriptions, or schemas.
const toolsListFullSHA256 = "ef0c112a9e77cf4a83f0b6b71ced71326ad881a9341ae6cba6722808a648c6c1"

func TestToolsListDefaultSurfaceBytesPinned(t *testing.T) {
	s := testServer()
	resp := s.handleMessage(context.Background(), &jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"}, "")
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/list response = %+v, want result", resp)
	}
	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal tools/list result: %v", err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != toolsListFullSHA256 {
		t.Fatalf("tools/list default-surface sha256 = %s (%d bytes), want %s", got, len(raw), toolsListFullSHA256)
	}
}
