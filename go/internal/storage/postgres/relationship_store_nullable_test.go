package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/relationships"
)

func TestRelationshipStoreGetResolvedRelationshipsForGeneration_AllowsNullTargetRepoID(t *testing.T) {
	t.Parallel()

	store := NewRelationshipStore(strictResolvedQueryDB{
		rows: &strictResolvedRows{
			data: [][]any{
				{
					"repository:r_source",
					nil,
					"repository:r_source",
					"platform:kubernetes:none:server/kubernetes.default.svc:none:none",
					string(relationships.RelRunsOn),
					0.97,
					1,
					"ArgoCD destination points at the runtime platform where the deployed repository runs",
					string(relationships.ResolutionSourceInferred),
					[]byte(`{"evidence_kinds":["ARGOCD_DESTINATION_PLATFORM"]}`),
				},
			},
		},
	})

	result, err := store.GetResolvedRelationshipsForGeneration(
		context.Background(),
		"git-repository-scope:repository:r_dde9aa64",
		"gen-1",
	)
	if err != nil {
		t.Fatalf("GetResolvedRelationshipsForGeneration() error = %v, want nil", err)
	}
	if got, want := len(result), 1; got != want {
		t.Fatalf("len(result) = %d, want %d", got, want)
	}
	if got := result[0].TargetRepoID; got != "" {
		t.Fatalf("TargetRepoID = %q, want empty string for NULL repo target", got)
	}
	if got, want := result[0].TargetEntityID, "platform:kubernetes:none:server/kubernetes.default.svc:none:none"; got != want {
		t.Fatalf("TargetEntityID = %q, want %q", got, want)
	}
	if got, want := result[0].RelationshipType, relationships.RelRunsOn; got != want {
		t.Fatalf("RelationshipType = %q, want %q", got, want)
	}
}

type strictResolvedQueryDB struct {
	rows Rows
}

func (db strictResolvedQueryDB) QueryContext(_ context.Context, _ string, _ ...any) (Rows, error) {
	return db.rows, nil
}

func (strictResolvedQueryDB) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected ExecContext call")
}

type strictResolvedRows struct {
	data [][]any
	idx  int
}

func (r *strictResolvedRows) Next() bool {
	r.idx++
	return r.idx <= len(r.data)
}

func (r *strictResolvedRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.data) {
		return fmt.Errorf("scan out of range")
	}
	row := r.data[r.idx-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan: got %d dest, have %d cols", len(dest), len(row))
	}
	for i, val := range row {
		switch d := dest[i].(type) {
		case *string:
			if val == nil {
				return fmt.Errorf("sql: Scan error on column index %d: converting NULL to string is unsupported", i)
			}
			*d = val.(string)
		case *float64:
			*d = val.(float64)
		case *int:
			*d = val.(int)
		case *[]byte:
			if val == nil {
				*d = nil
				continue
			}
			*d = val.([]byte)
		case *sql.NullString:
			if val == nil {
				*d = sql.NullString{}
				continue
			}
			*d = sql.NullString{String: val.(string), Valid: true}
		default:
			return fmt.Errorf("unsupported scan dest type %T", dest[i])
		}
	}
	return nil
}

func (*strictResolvedRows) Err() error   { return nil }
func (*strictResolvedRows) Close() error { return nil }
