// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func TestPackageManifestConsumptionKeyRowsHonorReducerAdmission(t *testing.T) {
	t.Parallel()

	rows := packageManifestConsumptionKeyRows([]facts.Envelope{
		manifestConsumptionTestEnvelope("accepted", false, map[string]any{
			"entity_type":     "Variable",
			"repo_id":         "repository:app",
			"entity_name":     "Friendly_Bard...Plugin",
			"config_kind":     "dependency",
			"package_manager": "python",
			"entity_metadata": map[string]any{
				"config_kind":     "not-a-dependency",
				"package_manager": "npm",
			},
		}),
		func() facts.Envelope {
			envelope := manifestConsumptionTestEnvelope("non-git", false, map[string]any{
				"entity_type": "Variable", "repo_id": "repository:app",
				"entity_name": "must-not-match", "config_kind": "dependency",
				"package_manager": "npm",
			})
			envelope.SourceRef.SourceSystem = "scanner"
			return envelope
		}(),
		manifestConsumptionTestEnvelope("ambiguous", false, map[string]any{
			"entity_type": "Variable",
			"repo_id":     "repository:app",
			"entity_name": "ignored",
			"entity_metadata": map[string]any{
				"config_kind":      "dependency",
				"package_manager":  "npm",
				"source_ambiguous": true,
			},
		}),
		manifestConsumptionTestEnvelope("unsupported", false, map[string]any{
			"entity_type": "Variable",
			"repo_id":     "repository:app",
			"entity_name": "ignored",
			"entity_metadata": map[string]any{
				"config_kind":                  "dependency",
				"package_manager":              "npm",
				"lockfile_unsupported_feature": "patch",
			},
		}),
		manifestConsumptionTestEnvelope("tombstone", true, map[string]any{
			"entity_type": "Variable",
			"repo_id":     "repository:app",
			"entity_name": "ignored",
			"entity_metadata": map[string]any{
				"config_kind":     "dependency",
				"package_manager": "npm",
			},
		}),
		manifestConsumptionTestEnvelope("metadata-only-anchor", false, map[string]any{
			"entity_metadata": map[string]any{
				"entity_type":     "Variable",
				"repo_id":         "repository:app",
				"entity_name":     "must-not-match",
				"config_kind":     "dependency",
				"package_manager": "npm",
			},
		}),
	})

	want := []packageManifestConsumptionKeyRow{
		{
			FactID:       "accepted",
			ScopeID:      "git-repository-scope:repository:app",
			GenerationID: "generation-1",
			RepositoryID: "repository:app",
			Ecosystem:    "pypi",
			PackageName:  "friendly-bard-plugin",
		},
		{
			FactID:       "accepted",
			ScopeID:      "git-repository-scope:repository:app",
			GenerationID: "generation-1",
			RepositoryID: "repository:app",
			Ecosystem:    "pypi",
			PackageName:  "friendly_bard...plugin",
		},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("packageManifestConsumptionKeyRows() = %#v, want %#v", rows, want)
	}
}

func TestRefreshPackageManifestConsumptionKeysReplacesEveryAcceptedFact(t *testing.T) {
	t.Parallel()

	database := &manifestConsumptionExecRecorder{}
	err := refreshPackageManifestConsumptionKeys(context.Background(), database, []facts.Envelope{
		manifestConsumptionTestEnvelope("live", false, map[string]any{
			"entity_type": "Variable",
			"repo_id":     "repository:app",
			"entity_name": "example",
			"entity_metadata": map[string]any{
				"config_kind":     "dependency",
				"package_manager": "npm",
			},
		}),
		manifestConsumptionTestEnvelope("tombstone", true, map[string]any{}),
	})
	if err != nil {
		t.Fatalf("refreshPackageManifestConsumptionKeys() error = %v, want nil", err)
	}
	if got, want := len(database.execs), 2; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(database.execs[0].query, "DELETE FROM package_manifest_consumption_keys") {
		t.Fatalf("first query = %q, want sidecar delete", database.execs[0].query)
	}
	if !strings.Contains(database.execs[1].query, "INSERT INTO package_manifest_consumption_keys") {
		t.Fatalf("second query = %q, want sidecar insert", database.execs[1].query)
	}
	if got, want := len(database.execs[1].args), 6; got != want {
		t.Fatalf("insert args = %d, want %d", got, want)
	}
}

func manifestConsumptionTestEnvelope(factID string, tombstone bool, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:       factID,
		ScopeID:      "git-repository-scope:repository:app",
		GenerationID: "generation-1",
		FactKind:     "content_entity",
		IsTombstone:  tombstone,
		Payload:      payload,
		SourceRef:    facts.Ref{SourceSystem: "git"},
	}
}

type manifestConsumptionExecRecorder struct {
	execs []manifestConsumptionExecCall
}

type manifestConsumptionExecCall struct {
	query string
	args  []any
}

func (r *manifestConsumptionExecRecorder) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	r.execs = append(r.execs, manifestConsumptionExecCall{query: query, args: append([]any(nil), args...)})
	return nil, nil
}

func (r *manifestConsumptionExecRecorder) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("manifestConsumptionExecRecorder.QueryContext must not be called")
}
