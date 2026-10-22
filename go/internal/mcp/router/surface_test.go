// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import "testing"

func TestParseSurface(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		in      string
		want    Surface
		wantErr bool
	}{
		{name: "empty is full", in: "", want: SurfaceFull},
		{name: "full", in: "full", want: SurfaceFull},
		{name: "router", in: "router", want: SurfaceRouter},
		{name: "core_router", in: "core_router", want: SurfaceCoreRouter},
		{name: "mixed case", in: "Core_Router", want: SurfaceCoreRouter},
		{name: "upper case", in: "ROUTER", want: SurfaceRouter},
		{name: "unknown", in: "everything", wantErr: true},
		{name: "hyphen is not underscore", in: "core-router", wantErr: true},
		{name: "padded is unknown", in: " router ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseSurface(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSurface(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("ParseSurface(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSurfaceValues(t *testing.T) {
	t.Parallel()
	for s, want := range map[Surface]string{
		SurfaceFull: "full", SurfaceRouter: "router", SurfaceCoreRouter: "core_router",
	} {
		if string(s) != want {
			t.Fatalf("Surface %q, want wire value %q", s, want)
		}
	}
}
