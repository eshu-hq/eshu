// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"strings"
	"testing"
)

func TestPostgresRoleBoundaryRequiresWorkloadAwareOpener(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "ESHU_POSTGRES_ROLE_CONTRACT_JSON":
			return `{"policy_revision":0}`
		case "ESHU_POSTGRES_DSN":
			return "invalid-dsn"
		default:
			return ""
		}
	}
	if db, err := OpenPostgres(context.Background(), getenv); err == nil || !strings.Contains(err.Error(), "OpenPostgresForWorkload") {
		if db != nil {
			_ = db.Close()
		}
		t.Fatalf("generic opener error = %v, want workload requirement before dial", err)
	}
	if db, err := OpenPostgresForWorkload(context.Background(), getenv, PostgresWorkloadCoordinator); err == nil || !strings.Contains(err.Error(), "ESHU_POSTGRES_ROLE_CONTRACT_JSON") {
		if db != nil {
			_ = db.Close()
		}
		t.Fatalf("workload opener error = %v, want contract parse before dial", err)
	}
}

func TestPostgresRoleBoundaryRequiredRejectsEmptyContractBeforeDial(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "ESHU_POSTGRES_ROLE_BOUNDARY_REQUIRED":
			return "true"
		case "ESHU_POSTGRES_DSN":
			return "invalid-dsn"
		default:
			return ""
		}
	}
	if db, err := OpenPostgresForWorkload(context.Background(), getenv, PostgresWorkloadRuntime); err == nil || !strings.Contains(err.Error(), "ESHU_POSTGRES_ROLE_CONTRACT_JSON") {
		if db != nil {
			_ = db.Close()
		}
		t.Fatalf("required role boundary error = %v, want empty contract denial before dial", err)
	}
	if db, err := OpenPostgres(context.Background(), getenv); err == nil || !strings.Contains(err.Error(), "OpenPostgresForWorkload") {
		if db != nil {
			_ = db.Close()
		}
		t.Fatalf("generic opener error = %v, want role boundary denial before dial", err)
	}
}

func TestAdvisoryProducerRequiresDedicatedCredentialBeforeDial(t *testing.T) {
	const assertion = `{"policy_revision":1,"database_identity":"f9092fbb-5341-4a59-96c1-061e1a993ed9","database":"eshu","routes":{"producer":{"contract_id":"advisory-producer-1","session_role":"advisory_login","effective_role":"advisory_login"}}}`
	tests := []struct {
		name     string
		writer   string
		producer string
		contract string
		want     string
	}{
		{name: "missing contract", writer: "writer-dsn", producer: "producer-dsn", want: "ESHU_POSTGRES_ROLE_CONTRACT_JSON"},
		{name: "missing producer credential", writer: "writer-dsn", contract: assertion, want: "ESHU_POSTGRES_PRODUCER_DSN"},
		{name: "shared writer credential", writer: "same-dsn", producer: "same-dsn", contract: assertion, want: "must differ"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(key string) string {
				switch key {
				case "ESHU_POSTGRES_DSN":
					return tc.writer
				case "ESHU_POSTGRES_PRODUCER_DSN":
					return tc.producer
				case "ESHU_POSTGRES_ROLE_CONTRACT_JSON":
					return tc.contract
				default:
					return ""
				}
			}
			if db, err := OpenPostgresForWorkload(context.Background(), getenv, PostgresWorkloadAdvisoryProducer); err == nil || !strings.Contains(err.Error(), tc.want) {
				if db != nil {
					_ = db.Close()
				}
				t.Fatalf("producer opener error = %v, want %q before dial", err, tc.want)
			}
		})
	}
}
