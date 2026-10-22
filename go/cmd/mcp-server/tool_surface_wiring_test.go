// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

func TestLoadToolSurface(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    router.Surface
		wantErr bool
	}{
		{name: "unset defaults to full", value: "", want: router.SurfaceFull},
		{name: "full", value: "full", want: router.SurfaceFull},
		{name: "router", value: "router", want: router.SurfaceRouter},
		{name: "core_router", value: "core_router", want: router.SurfaceCoreRouter},
		{name: "surrounding whitespace is trimmed", value: " router\n", want: router.SurfaceRouter},
		{name: "unknown value refuses", value: "everything", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string {
				if key == "ESHU_MCP_TOOL_SURFACE" {
					return tt.value
				}
				return ""
			}
			got, err := loadToolSurface(getenv)
			if (err != nil) != tt.wantErr {
				t.Fatalf("loadToolSurface(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if tt.wantErr {
				for _, allowed := range []string{"full", "router", "core_router"} {
					if !strings.Contains(err.Error(), allowed) {
						t.Fatalf("error %q does not list allowed value %q", err, allowed)
					}
				}
				return
			}
			if got != tt.want {
				t.Fatalf("loadToolSurface(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
