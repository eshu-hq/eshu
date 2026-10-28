// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type ecsRows struct{ returned bool }

func (r *ecsRows) Next() bool {
	if r.returned {
		return false
	}
	r.returned = true
	return true
}

func (r *ecsRows) Scan(dest ...any) error {
	*dest[0].(*string) = "service-fact"
	*dest[1].(*string) = "scope-a"
	*dest[2].(*string) = "gen-a"
	*dest[3].(*[]byte) = []byte(`{"arn":"arn:aws:ecs:us-east-1:000000000000:service/c/api","name":"api","attributes":{"running_count":0}}`)
	*dest[4].(*[]byte) = nil
	return nil
}
func (r *ecsRows) Err() error   { return nil }
func (r *ecsRows) Close() error { return nil }

type ecsQueryer struct{}

func (ecsQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return &ecsRows{}, nil
}

func TestECSTaskStoreRejectsServiceWithoutClusterIdentity(t *testing.T) {
	store := NewPostgresECSTaskStore(ecsQueryer{})
	got, err := store.ListRunningTasks(context.Background(), "arn:aws:ecs:us-east-1:000000000000:service/c/api", querycontract.RepositoryAccessFilter{AllScopes: true})
	if err == nil && got != nil {
		t.Fatalf("service without cluster identity returned %#v; want fail-closed", got)
	}
}

func TestValidECSTaskIdentityRejectsCrossClusterAndAccount(t *testing.T) {
	service := "arn:aws:ecs:us-east-1:000000000000:service/prod/api"
	for _, tc := range []struct {
		task string
		want bool
	}{
		{"arn:aws:ecs:us-east-1:000000000000:task/prod/abc", true},
		{"arn:aws:ecs:us-east-1:000000000000:task/other/abc", false},
		{"arn:aws:ecs:us-east-1:999999999999:task/prod/abc", false},
		{"arn:aws:ecs:us-west-2:000000000000:task/prod/abc", false},
	} {
		if got := validECSTaskIdentity(service, tc.task); got != tc.want {
			t.Fatalf("task %q valid=%t, want %t", tc.task, got, tc.want)
		}
	}
}
