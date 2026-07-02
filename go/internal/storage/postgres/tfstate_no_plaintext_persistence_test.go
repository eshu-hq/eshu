// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/collector/tfstateruntime"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestTerraformStateClaimedCommitDoesNotPersistPlaintextSecrets(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 10, 17, 30, 0, 0, time.UTC)
	statePathToken := "path-token-must-not-persist"
	outputSecret := "output-secret-must-not-persist"
	attributeSecret := "attribute-secret-must-not-persist"
	tagSecret := "tag-secret-must-not-persist"
	statePath := writeNoPlaintextState(t, statePathToken, fmt.Sprintf(`{
		"format_version": "1.0",
		"terraform_version": "1.9.8",
		"serial": 17,
		"lineage": "lineage-no-plaintext",
		"outputs": {
			"admin_password": {"sensitive": false, "value": %q}
		},
		"resources": [{
			"mode": "managed",
			"type": "aws_instance",
			"name": "app",
			"provider": "provider[\"registry.terraform.io/hashicorp/aws\"]",
			"instances": [{
				"attributes": {
					"id": "i-1234567890",
					"password": %q,
					"tags": {"secret_tag": %q}
				}
			}]
		}]
	}`, outputSecret, attributeSecret, tagSecret))

	scopeValue, err := scope.NewTerraformStateSnapshotScope(
		"platform-infra",
		string(terraformstate.BackendLocal),
		statePath,
		map[string]string{"repo_id": "platform-infra"},
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
	}
	generation, err := scope.NewTerraformStateSnapshotGeneration(
		scopeValue.ScopeID,
		17,
		"lineage-no-plaintext",
		observedAt,
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotGeneration() error = %v, want nil", err)
	}
	key, err := redact.NewKey([]byte("no-plaintext-proof-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	rules, err := redact.NewRuleSet("no-plaintext-proof", []string{
		"admin_password",
		"password",
		"secret_tag",
	})
	if err != nil {
		t.Fatalf("NewRuleSet() error = %v, want nil", err)
	}
	source := tfstateruntime.ClaimedSource{
		Resolver: terraformstate.DiscoveryResolver{
			Config: terraformstate.DiscoveryConfig{
				Seeds: []terraformstate.DiscoverySeed{{
					Kind:   terraformstate.BackendLocal,
					Path:   statePath,
					RepoID: "platform-infra",
				}},
			},
		},
		SourceFactory:  tfstateruntime.DefaultSourceFactory{},
		RedactionKey:   key,
		RedactionRules: rules,
		Clock:          func() time.Time { return observedAt },
	}
	item := workflow.WorkItem{
		WorkItemID:          "tfstate-no-plaintext-work",
		RunID:               "run-no-plaintext",
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "collector-tfstate-primary",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             scopeValue.ScopeID,
		AcceptanceUnitID:    "platform-infra",
		SourceRunID:         generation.GenerationID,
		GenerationID:        generation.GenerationID,
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentFencingToken: 42,
		CreatedAt:           observedAt,
		UpdatedAt:           observedAt,
	}

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	db := &fakeTransactionalDB{
		tx: &fakeTx{},
		queryResponses: []queueFakeRows{{
			rows: nil,
		}},
	}
	store := NewIngestionStore(db)
	store.Now = func() time.Time { return observedAt }

	err = store.CommitClaimedScopeGenerationWithStreamError(
		context.Background(),
		workflow.ClaimMutation{
			WorkItemID:    item.WorkItemID,
			ClaimID:       "claim-no-plaintext",
			FencingToken:  item.CurrentFencingToken,
			OwnerID:       "collector-owner",
			ObservedAt:    observedAt,
			LeaseDuration: time.Minute,
		},
		collected.Scope,
		collected.Generation,
		collected.Facts,
		collected.FactStreamErr,
	)
	if err != nil {
		t.Fatalf("CommitClaimedScopeGenerationWithStreamError() error = %v, want nil", err)
	}
	if !db.tx.committed {
		t.Fatal("transaction committed = false, want true")
	}

	assertExecArgsDoNotContain(t, persistedArgLists(db.tx.execs, db.tx.queries), []string{
		statePathToken,
		outputSecret,
		attributeSecret,
		tagSecret,
	})
}

func writeNoPlaintextState(t *testing.T, pathToken string, content string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), pathToken)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v, want nil", err)
	}
	path := filepath.Join(dir, "terraform.tfstate")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}
	return path
}

// persistedArgLists flattens execs and queries into one ordered list of
// argument lists so plaintext-persistence assertions can scan every value the
// production code sent to Postgres, regardless of whether the statement ran
// through ExecContext or QueryContext. The fact_records upsert runs as a
// query (INSERT ... RETURNING fact_id, issue #4444 review, codex P1), so a
// redaction proof that only scanned execs would silently stop covering the
// fact payload.
func persistedArgLists(execs []fakeExecCall, queries []fakeQueryCall) [][]any {
	argLists := make([][]any, 0, len(execs)+len(queries))
	for _, exec := range execs {
		argLists = append(argLists, exec.args)
	}
	for _, query := range queries {
		argLists = append(argLists, query.args)
	}
	return argLists
}

func assertExecArgsDoNotContain(t *testing.T, argLists [][]any, needles []string) {
	t.Helper()
	for listIndex, args := range argLists {
		for argIndex, arg := range args {
			text := persistedArgText(arg)
			for _, needle := range needles {
				if strings.Contains(text, needle) {
					t.Fatalf(
						"call[%d].args[%d] persisted plaintext %q in %s",
						listIndex,
						argIndex,
						needle,
						text,
					)
				}
			}
		}
	}
}

func persistedArgText(value any) string {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}
