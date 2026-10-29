// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"strings"
	"testing"
)

func TestLoadPostgresRoleContractRequiresExactAssertions(t *testing.T) {
	const valid = `{"policy_revision":7,"database_identity":"91d1dfab-4726-449a-948e-d459ee2fd0eb","database":"eshu","routes":{"writer":{"contract_id":"runtime-writer-7","session_role":"eshu_runtime","effective_role":"eshu_runtime"}}}`
	contract, err := LoadPostgresRoleContract(func(key string) string {
		if key == "ESHU_POSTGRES_ROLE_CONTRACT_JSON" {
			return valid
		}
		return ""
	}, PostgresWorkloadRuntime, PostgresRouteWriter)
	if err != nil {
		t.Fatal(err)
	}
	if contract == nil || contract.PolicyRevision != 7 || contract.Route.ContractID != "runtime-writer-7" {
		t.Fatalf("loaded contract = %+v", contract)
	}
	for _, raw := range []string{
		`{"policy_revision":0,"database_identity":"91d1dfab-4726-449a-948e-d459ee2fd0eb","database":"eshu","routes":{"writer":{"contract_id":"x","session_role":"r","effective_role":"r"}}}`,
		`{"policy_revision":7,"database_identity":"bad","database":"eshu","routes":{"writer":{"contract_id":"x","session_role":"r","effective_role":"r"}}}`,
		`{"policy_revision":7,"database_identity":"91d1dfab-4726-449a-948e-d459ee2fd0eb","database":"eshu","routes":{}}`,
		`{"policy_revision":7,"database_identity":"91d1dfab-4726-449a-948e-d459ee2fd0eb","database":"eshu","routes":{"writer":{"contract_id":"x","session_role":"r","effective_role":"r","profile":"owner"}}}`,
	} {
		_, err := LoadPostgresRoleContract(func(key string) string {
			if key == "ESHU_POSTGRES_ROLE_CONTRACT_JSON" {
				return raw
			}
			return ""
		}, PostgresWorkloadRuntime, PostgresRouteWriter)
		if err == nil || strings.Contains(err.Error(), raw) {
			t.Fatalf("raw contract accepted or leaked: %v", err)
		}
	}
}
