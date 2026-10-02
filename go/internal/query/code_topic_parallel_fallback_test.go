// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestInvestigateCodeTopicParallelDoesNotRetryOtherFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "snapshot setup", err: errors.New("seeded snapshot setup failure")},
		{name: "driver deadline", err: context.DeadlineExceeded},
		{name: "missing checkpoint", err: errors.New("seeded missing checkpoint")},
		{name: "identity failure", err: errors.New("seeded identity failure")},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := &codeTopicParallelRecorder{}
			pool := openCodeTopicParallelDB(t, recorder)
			reader := NewContentReaderWithReadStore(codeTopicTestSnapshotStore{
				ReadStore: postgres.NewSQLReadStore(pool), handle: pool, beginErr: test.err,
			})
			terms := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
			rows, err := reader.InvestigateCodeTopic(t.Context(), codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 26})
			if !errors.Is(err, test.err) || len(rows) != 0 {
				t.Fatalf("non-capacity error retried: rows=%#v err=%v", rows, err)
			}
			recorder.mu.Lock()
			defer recorder.mu.Unlock()
			if recorder.probes != 0 || recorder.serialQueries != 0 || recorder.assemblyJSON != "" || recorder.active != 0 {
				t.Fatalf("unexpected SQL: probes=%d serial=%d assembly=%q active=%d", recorder.probes, recorder.serialQueries, recorder.assemblyJSON, recorder.active)
			}
		})
	}
}
