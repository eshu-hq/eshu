// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package s3

import (
	"context"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// The fakes below are leaf-local copies of the root cypher package's test
// doubles. Test fakes cannot be imported across the package split, so each
// side carries its own copy; keep the copies behavior-identical.

type recordingExecutor struct {
	calls     []sourcecypher.Statement
	errAtCall error

	// readCalls records every Reader.Run invocation (used by shell-exec
	// orphan-cleanup retract tests, the only production path that reads
	// through EdgeWriter.Reader). readCandidates/readConnected script the S1
	// candidate keys and which of those currently have a relationship (S2);
	// both are nil by default, so a test that never exercises shell-exec
	// cleanup reads sees zero candidates and the production code performs no
	// further read or write.
	readCalls      []sourcecypher.Statement
	readCandidates []string
	readConnected  map[string]bool
	readErr        error
}

func (r *recordingExecutor) Execute(_ context.Context, statement sourcecypher.Statement) error {
	r.calls = append(r.calls, statement)
	if r.errAtCall != nil {
		return r.errAtCall
	}

	return nil
}

// Run implements sourcecypher.OrphanSweepReader so recordingExecutor can double
// as the EdgeWriter.Reader in shell-exec orphan-cleanup retract tests. It
// distinguishes the S1 candidate-keys read (no "keys" parameter) from the S2
// connected-keys read (a "keys" parameter) the same way the production
// statements do.
func (r *recordingExecutor) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	r.readCalls = append(r.readCalls, sourcecypher.Statement{Cypher: cypher, Parameters: params})
	if r.readErr != nil {
		return nil, r.readErr
	}
	if keys, ok := params["keys"].([]string); ok {
		rows := make([]map[string]any, 0, len(keys))
		for _, k := range keys {
			if r.readConnected[k] {
				rows = append(rows, map[string]any{"key": k})
			}
		}
		return rows, nil
	}
	rows := make([]map[string]any, 0, len(r.readCandidates))
	for _, k := range r.readCandidates {
		rows = append(rows, map[string]any{"key": k})
	}
	return rows, nil
}

// noopGroupExecutor records nothing and returns nil so the benchmark measures
// statement construction and batching cost, not backend round trips.
type noopGroupExecutor struct{}

func (noopGroupExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (noopGroupExecutor) ExecuteGroup(context.Context, []sourcecypher.Statement) error {
	return nil
}

// echoingPostureExistenceReader is the default PostureExistenceReader test
// double for the posture node writers. With ExistingUIDs left nil it
// answers every candidate uid in the read as existing, so most writer tests
// do not need bespoke read-existence wiring; set ExistingUIDs to restrict
// which uids are reported as existing (used by never-create tests).
type echoingPostureExistenceReader struct {
	ExistingUIDs map[string]bool
	err          error
	calls        []map[string]any
}

func (r *echoingPostureExistenceReader) Run(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
	r.calls = append(r.calls, params)
	if r.err != nil {
		return nil, r.err
	}
	candidates, _ := params["candidate_uids"].([]any)
	rows := make([]map[string]any, 0, len(candidates))
	for _, c := range candidates {
		uid, _ := c.(string)
		if uid == "" {
			continue
		}
		if r.ExistingUIDs != nil && !r.ExistingUIDs[uid] {
			continue
		}
		rows = append(rows, map[string]any{"existing_uid": uid})
	}
	return rows, nil
}
