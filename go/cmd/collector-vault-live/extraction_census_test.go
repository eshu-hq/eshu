// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/vaultlive"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// censusRecordRel is the #8869 AC2 extraction census record, relative to the
// repository root. It lives in this command's testdata so the asserting tests
// do not depend on committed evidence files. It must name every source family
// the cutover moves and the credential boundary the extraction must preserve.
const censusRecordRel = "go/cmd/collector-vault-live/testdata/8869-collector-vault-live-census.md"

// censusSourceFamilies are the package directories the census record must
// account for. They are asserted against the tree so the record cannot drift
// silently when a family moves before extraction.
var censusSourceFamilies = []string{
	"go/cmd/collector-vault-live",
	"go/internal/collector/vaultlive",
	"go/internal/collector/vaultlive/vaultapi",
	"go/internal/collector/access/posture",
}

// censusRecordMustName lists strings the census record must contain: the scope
// identity, the fact-kind vocabulary, and the credential-handling contract.
var censusRecordMustName = []string{
	"vault_live",
	"vault_auth_mount",
	"vault_auth_role",
	"vault_acl_policy",
	"vault_identity_entity",
	"vault_identity_alias",
	"vault_kv_metadata",
	"vault_secret_engine_mount",
	"token_env",
	"ESHU_VAULT_LIVE_REDACTION_KEY",
}

func repoRootForCensus(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file is go/cmd/collector-vault-live/extraction_census_test.go.
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		return filepath.Dir(root)
	}
	return root
}

func TestExtractionCensusRecordNamesSourceFamilies(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRootForCensus(t), censusRecordRel))
	if err != nil {
		t.Fatalf("read census record %s: %v", censusRecordRel, err)
	}
	text := string(body)
	for _, family := range censusSourceFamilies {
		if !strings.Contains(text, family) {
			t.Errorf("census record %s does not name source family %s", censusRecordRel, family)
		}
	}
	for _, want := range censusRecordMustName {
		if !strings.Contains(text, want) {
			t.Errorf("census record %s does not name %q", censusRecordRel, want)
		}
	}
}

func TestExtractionCensusPackagesExist(t *testing.T) {
	root := repoRootForCensus(t)
	for _, family := range censusSourceFamilies {
		info, err := os.Stat(filepath.Join(root, family))
		if err != nil {
			t.Errorf("census family %s missing from tree: %v", family, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("census family %s is not a directory", family)
		}
	}
}

// censusPostReplayMustName lists strings the census record must contain once
// the #8869 rollback drill and post-replay verification complete: the drill
// PR pair, the surviving active generation, the observed scope identity, and
// the verified worker deployment. They pin the record to the drill the owner
// ran on 2026-10-28 so a later edit cannot silently drop the evidence.
var censusPostReplayMustName = []string{
	"iac-eks-eshu#402",
	"iac-eks-eshu#404",
	"68790ab7",
	"vault_live:a542fa8a",
	"sha-a93fb67440a1e2f6f9626b2c69e7e9066ec46b03",
}

func TestExtractionCensusRecordsPostReplayVerification(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRootForCensus(t), censusRecordRel))
	if err != nil {
		t.Fatalf("read census record %s: %v", censusRecordRel, err)
	}
	text := string(body)
	for _, want := range censusPostReplayMustName {
		if !strings.Contains(text, want) {
			t.Errorf("census record %s does not name post-replay evidence %q", censusRecordRel, want)
		}
	}
}

// censusTruthReadMustName lists strings the census record must contain once
// the API/MCP truth-read surface is recorded from code inspection: all five
// scope-anchored HTTP route slugs, all seven MCP tool names, the scope_id-
// required counts-only wording, the vault_secret/vault_lease absence note,
// the ledger zero-consumer note, and the explicit live-read gap. Each entry
// pins one load-bearing claim so a later edit that drops any of them fails
// this test instead of silently shrinking the record.
var censusTruthReadMustName = []string{
	"identity-trust-chains",
	"privilege-posture-observations",
	"secret-access-paths",
	"posture-gaps",
	"posture-summary",
	"list_secrets_iam_identity_trust_chains",
	"list_secrets_iam_privilege_posture_observations",
	"list_secrets_iam_secret_access_paths",
	"list_secrets_iam_posture_gaps",
	"count_secrets_iam_posture",
	"get_changed_since",
	"get_generation_lifecycle",
	"counts-only",
	"scope_id",
	"vault_secret",
	"vault_lease",
	"kind_disclosure_ledger",
	"zero real-consumer",
	"localhost:8080/healthz",
	"NOT_CHECKED",
}

func TestExtractionCensusRecordsTruthReadSurface(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRootForCensus(t), censusRecordRel))
	if err != nil {
		t.Fatalf("read census record %s: %v", censusRecordRel, err)
	}
	text := string(body)
	for _, want := range censusTruthReadMustName {
		if !strings.Contains(text, want) {
			t.Errorf("census record %s does not name truth-read evidence %q", censusRecordRel, want)
		}
	}
}

func TestExtractionScopeIdentityAgrees(t *testing.T) {
	if string(scope.CollectorVaultLive) != vaultlive.CollectorKind {
		t.Errorf("scope identity %q != vaultlive source identity %q",
			string(scope.CollectorVaultLive), vaultlive.CollectorKind)
	}
}

// TestExtractionBoundaryConfigCarriesNoToken guards the cutover contract: the
// config that crosses from the command into the collector package (and later
// into the extracted repository) must carry non-secret identity only. Tokens
// stay in the command-local client factory, resolved from the environment.
func TestExtractionBoundaryConfigCarriesNoToken(t *testing.T) {
	banned := []string{"token", "secret", "password", "credential"}
	for _, typ := range []reflect.Type{
		reflect.TypeOf(vaultlive.ClusterTarget{}),
		reflect.TypeOf(vaultlive.Config{}),
	} {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			// FencingToken is a monotonic int64 claim-generation counter, not
			// secret material; lock the type so a future string-typed token
			// under this name cannot slip through.
			if field.Name == "FencingToken" {
				if field.Type.Kind() != reflect.Int64 {
					t.Errorf("cross-boundary %s.FencingToken must stay an int64 generation counter, got %s",
						typ.Name(), field.Type)
				}
				continue
			}
			name := strings.ToLower(field.Name)
			for _, word := range banned {
				if strings.Contains(name, word) {
					t.Errorf("cross-boundary %s.%s carries %q material; tokens must stay in the command-local factory",
						typ.Name(), field.Name, word)
				}
			}
		}
	}
}

// TestExtractionBoundaryKeyMaterialIsExplicit names the single permitted
// exception to the no-secret boundary: the deployment-scoped HMAC redaction
// key. redact.Key is secret material by its own package definition; it
// crosses the boundary by design because the snapshot source redacts with
// it, and the census records the exception. Any other key-material field,
// or the same type under a different name, fails.
func TestExtractionBoundaryKeyMaterialIsExplicit(t *testing.T) {
	keyType := reflect.TypeOf(redact.Key{})
	structs := []struct {
		name string
		typ  reflect.Type
	}{
		{"ClusterTarget", reflect.TypeOf(vaultlive.ClusterTarget{})},
		{"Config", reflect.TypeOf(vaultlive.Config{})},
	}
	found := false
	for _, tc := range structs {
		for i := 0; i < tc.typ.NumField(); i++ {
			field := tc.typ.Field(i)
			if field.Type != keyType {
				continue
			}
			if tc.name != "Config" || field.Name != "RedactionKey" {
				t.Errorf("cross-boundary %s.%s carries key material outside the documented RedactionKey exception", tc.name, field.Name)
				continue
			}
			found = true
		}
	}
	// The exception must exist: if Config stops carrying the redaction key,
	// the census exception claim is stale.
	if !found {
		t.Errorf("Config.RedactionKey of type redact.Key not found; update the census exception claim")
	}
}

func TestExtractionAuthKeyScopesByClusterAndNamespace(t *testing.T) {
	sameCluster := authKey("cluster-a", "ns-1")
	if sameCluster == authKey("cluster-a", "ns-2") {
		t.Error("authKey must distinguish namespaces within one cluster")
	}
	if sameCluster == authKey("cluster-b", "ns-1") {
		t.Error("authKey must distinguish clusters for one namespace")
	}
}
