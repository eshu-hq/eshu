// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"reflect"
	"strings"
	"testing"
)

func loadSnapshotExportConfig(raw string) (Config, error) {
	return LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return "postgres://user:secret@writer/db"
		case "ESHU_POSTGRES_READ_SNAPSHOT_EXPORT_BUILDS":
			return raw
		default:
			return ""
		}
	})
}

// TestLoadConfigParsesSnapshotExportBuilds pins the #7957 declaration shape:
// an unset or blank value is deny-by-default (no declarations), and a valid
// array is kept exactly, with no trimming of server_version.
func TestLoadConfigParsesSnapshotExportBuilds(t *testing.T) {
	maxEvidence := strings.Repeat("e", 256)
	for _, tc := range []struct {
		name string
		raw  string
		want []SnapshotExportBuild
	}{
		{name: "unset", raw: "", want: nil},
		{name: "blank", raw: "  \t", want: nil},
		{
			name: "one build", raw: `[{"server_version":"18.6 (eshu-pg 18.6-r2)","evidence":"sha256:abc image digest"}]`,
			want: []SnapshotExportBuild{{ServerVersion: "18.6 (eshu-pg 18.6-r2)", Evidence: "sha256:abc image digest"}},
		},
		{
			name: "bare label kept", raw: `[{"server_version":"18.6","evidence":"unpatched CI mirror; guard plumbing only"}]`,
			want: []SnapshotExportBuild{{ServerVersion: "18.6", Evidence: "unpatched CI mirror; guard plumbing only"}},
		},
		{
			name: "server_version not trimmed", raw: `[{"server_version":" 18.6","evidence":"x"}]`,
			want: []SnapshotExportBuild{{ServerVersion: " 18.6", Evidence: "x"}},
		},
		{
			name: "evidence at limit", raw: `[{"server_version":"18.6","evidence":"` + maxEvidence + `"}]`,
			want: []SnapshotExportBuild{{ServerVersion: "18.6", Evidence: maxEvidence}},
		},
		{
			name: "two builds", raw: `[{"server_version":"18.6","evidence":"a"},{"server_version":"18.6 (r2)","evidence":"b"}]`,
			want: []SnapshotExportBuild{{ServerVersion: "18.6", Evidence: "a"}, {ServerVersion: "18.6 (r2)", Evidence: "b"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadSnapshotExportConfig(tc.raw)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if !reflect.DeepEqual(cfg.SnapshotExportBuilds, tc.want) {
				t.Fatalf("SnapshotExportBuilds = %#v, want %#v", cfg.SnapshotExportBuilds, tc.want)
			}
		})
	}
}

// TestLoadConfigRejectsInvalidSnapshotExportBuilds pins each #7957 rejection
// rule. Every rejection fails LoadConfig before any dial, names the variable,
// and never echoes a DSN credential.
func TestLoadConfigRejectsInvalidSnapshotExportBuilds(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "empty array", raw: `[]`},
		{name: "null", raw: `null`},
		{name: "malformed json", raw: `[{"server_version":"18.6","evidence":"x"`},
		{name: "object not array", raw: `{"server_version":"18.6","evidence":"x"}`},
		{name: "missing evidence", raw: `[{"server_version":"18.6"}]`},
		{name: "empty evidence", raw: `[{"server_version":"18.6","evidence":""}]`},
		{name: "evidence over limit", raw: `[{"server_version":"18.6","evidence":"` + strings.Repeat("e", 257) + `"}]`},
		{name: "missing server_version", raw: `[{"evidence":"x"}]`},
		{name: "empty server_version", raw: `[{"server_version":"","evidence":"x"}]`},
		{name: "duplicate server_version", raw: `[{"server_version":"18.6","evidence":"a"},{"server_version":"18.6","evidence":"b"}]`},
		{name: "unknown field", raw: `[{"server_version":"18.6","evidence":"x","digest":"y"}]`},
		{name: "trailing data", raw: `[{"server_version":"18.6","evidence":"x"}] []`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadSnapshotExportConfig(tc.raw)
			if err == nil {
				t.Fatalf("LoadConfig accepted %q", tc.raw)
			}
			if !strings.Contains(err.Error(), "ESHU_POSTGRES_READ_SNAPSHOT_EXPORT_BUILDS") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("LoadConfig error = %v, want the variable name and no credentials", err)
			}
		})
	}
}
