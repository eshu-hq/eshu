// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestLoadConfigSplitsOneBudget(t *testing.T) {
	tests := []struct {
		name                                     string
		env                                      map[string]string
		readOpen, writeOpen, readIdle, writeIdle int
	}{
		{"default same endpoint", map[string]string{"ESHU_POSTGRES_DSN": "postgres://user:secret@writer/db"}, 15, 15, 5, 5},
		{"distinct endpoint", map[string]string{"ESHU_CONTENT_STORE_DSN": "postgres://user:secret@writer/db", "ESHU_POSTGRES_READ_DSN": "postgres://user:secret@reader/db"}, 15, 15, 5, 5},
		{"allocate reader", map[string]string{"ESHU_POSTGRES_DSN": "postgres://writer/db", "ESHU_POSTGRES_READ_MAX_OPEN_CONNS": "28"}, 28, 2, 8, 2},
		{"explicit idle", map[string]string{"ESHU_POSTGRES_DSN": "postgres://writer/db", "ESHU_POSTGRES_READ_MAX_OPEN_CONNS": "28", "ESHU_POSTGRES_READ_MAX_IDLE_CONNS": "9"}, 28, 2, 9, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadConfig(func(key string) string { return tc.env[key] })
			if err != nil {
				t.Fatal(err)
			}
			if cfg.ReadMaxOpenConns != tc.readOpen || cfg.WriterMaxOpenConns != tc.writeOpen || cfg.ReadMaxIdleConns != tc.readIdle || cfg.WriterMaxIdleConns != tc.writeIdle {
				t.Fatalf("pool split = %+v", cfg)
			}
			if cfg.ReadDSN == "" || cfg.WriterDSN == "" {
				t.Fatal("missing endpoint")
			}
		})
	}
}

func TestLoadConfigRejectsInvalidPoolsWithoutSecrets(t *testing.T) {
	for _, tc := range []map[string]string{
		{"ESHU_POSTGRES_MAX_OPEN_CONNS": "1"},
		{"ESHU_POSTGRES_READ_MAX_OPEN_CONNS": "30"},
		{"ESHU_POSTGRES_READ_MAX_OPEN_CONNS": "wrong"},
		{"ESHU_POSTGRES_READ_MAX_IDLE_CONNS": "11"},
		{"ESHU_POSTGRES_READ_MAX_IDLE_CONNS": "-1"},
	} {
		t.Run(strings.Join([]string{tc["ESHU_POSTGRES_MAX_OPEN_CONNS"], tc["ESHU_POSTGRES_READ_MAX_OPEN_CONNS"], tc["ESHU_POSTGRES_READ_MAX_IDLE_CONNS"]}, "/"), func(t *testing.T) {
			tc["ESHU_POSTGRES_DSN"] = "postgres://user:secret@writer/db"
			_, err := LoadConfig(func(key string) string { return tc[key] })
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("secret in error: %v", err)
			}
		})
	}
}

func TestLoadConfigAcceptsNativeCandidateHostsWithinOneBudget(t *testing.T) {
	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return "host=writer-a,writer-b port=5432,5432 user=proof dbname=eshu sslmode=disable"
		case "ESHU_POSTGRES_READ_DSN":
			return "host=reader-a,reader-b port=5432,5432 user=proof dbname=eshu sslmode=disable"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WriterMaxOpenConns+cfg.ReadMaxOpenConns != 30 || cfg.WriterMaxIdleConns+cfg.ReadMaxIdleConns != 10 {
		t.Fatalf("candidate pool budget=%+v", cfg)
	}
}

func TestLoadConfigRejectsInvalidExpectedSystemIDBeforeDial(t *testing.T) {
	_, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return "postgres://user:secret@writer/db"
		case "ESHU_POSTGRES_EXPECTED_SYSTEM_ID":
			return "not-a-system-id"
		default:
			return ""
		}
	})
	if err == nil || !strings.Contains(err.Error(), "ESHU_POSTGRES_EXPECTED_SYSTEM_ID") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("expected system ID validation=%v", err)
	}
}
