package main

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestBuildReducerServiceWiresReducerClaimDomains(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		reducerClaimDomainsEnv: strings.Join([]string{
			string(reducer.DomainSQLRelationshipMaterialization),
			string(reducer.DomainInheritanceMaterialization),
		}, ","),
	}
	getenv := func(key string) string { return env[key] }

	db := &fakeReducerDB{}
	service, err := buildReducerService(
		db,
		stubGraphExecutor{},
		stubCypherExecutor{},
		postgres.NewSharedIntentStore(db),
		stubCypherReader{},
		stubCypherReader{},
		getenv,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildReducerService() error = %v, want nil", err)
	}

	queue, ok := service.WorkSource.(postgres.ReducerQueue)
	if !ok {
		t.Fatalf("work source type = %T, want postgres.ReducerQueue", service.WorkSource)
	}
	want := []reducer.Domain{
		reducer.DomainSQLRelationshipMaterialization,
		reducer.DomainInheritanceMaterialization,
	}
	if !domainSlicesEqual(queue.ClaimDomains, want) {
		t.Fatalf("claim domains = %#v, want %#v", queue.ClaimDomains, want)
	}
}

func domainSlicesEqual(left, right []reducer.Domain) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
