// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestBuildReducerServiceWiresTheQueueRetryBudget guards the #8826 wiring: the
// Service decides deferred versus failed for an expected wait from
// RetryMaxAttempts, so it must equal the budget the queue dead-letters at.
// Dropping the assignment in buildReducerService leaves the field zero, which
// reads as the default of 3 and silently ignores an operator's
// ESHU_REDUCER_MAX_ATTEMPTS; the value 5 here fails that regression.
func TestBuildReducerServiceWiresTheQueueRetryBudget(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		env  map[string]string
		want int
	}{
		"configured": {env: map[string]string{"ESHU_REDUCER_MAX_ATTEMPTS": "5"}, want: 5},
		"default":    {env: map[string]string{}, want: 3},
	} {
		db := txFakeReducerDB{fakeReducerDB: &fakeReducerDB{}}
		service, err := buildReducerService(context.Background(), db, stubGraphExecutor{}, stubCypherExecutor{},
			postgres.NewSharedIntentStore(db.fakeReducerDB), stubCypherReader{}, stubCypherReader{},
			func(key string) string { return tc.env[key] }, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("%s: buildReducerService() error = %v", name, err)
		}
		if service.RetryMaxAttempts != tc.want {
			t.Fatalf("%s: Service.RetryMaxAttempts = %d, want %d", name, service.RetryMaxAttempts, tc.want)
		}
	}
}
