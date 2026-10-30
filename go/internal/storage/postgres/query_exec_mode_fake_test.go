// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import "github.com/jackc/pgx/v5"

// fakeQueryCall records one QueryContext call a test fake received.
//
// A reader that pins a pgx exec mode passes it as the first query argument,
// where pgx's stdlib driver strips it before binding. newFakeQueryCall strips
// it the same way, so args stays aligned with the SQL placeholders ($1 is
// args[0]) and execMode carries the pinned mode for tests that assert it.
type fakeQueryCall struct {
	query       string
	args        []any
	execMode    pgx.QueryExecMode
	hasExecMode bool
}

// newFakeQueryCall records query and args, moving a leading pgx.QueryExecMode
// argument into execMode the way the pgx stdlib driver consumes it.
func newFakeQueryCall(query string, args []any) fakeQueryCall {
	call := fakeQueryCall{query: query, args: args}
	if len(args) > 0 {
		if mode, ok := args[0].(pgx.QueryExecMode); ok {
			call.execMode = mode
			call.hasExecMode = true
			call.args = args[1:]
		}
	}
	return call
}
