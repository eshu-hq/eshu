// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"sort"
)

// down removes the demo stack along with its volumes and networks.
//
// `-v` and `--remove-orphans` are not optional niceties: the acceptance
// criteria for the demo are zero leaked containers, volumes, or networks, and
// a plain `compose down` leaves named volumes behind. Every invocation is
// scoped to the demo project, so a stack the demo did not create is out of
// reach here by construction rather than by care.
func (r *demoRuntime) down(ctx context.Context) error {
	if _, err := r.exec(ctx, nil, "docker", r.composeArgs("down", "-v", "--remove-orphans")...); err != nil {
		return fmt.Errorf("remove demo stack (project %q): %w", r.project, err)
	}
	return nil
}

// status reports whether the demo project is running and finished indexing.
//
// Running and ready are deliberately separate: a stack that is up but still
// indexing answers the demo questions incompletely, so reporting "running" as
// "ready" would be the same health-vs-completeness mistake first-run refuses
// to make.
func (r *demoRuntime) status(ctx context.Context) (demoResult, error) {
	res := demoResult{Project: r.project, PhaseMillis: map[string]int64{}}
	running, err := r.alreadyRunning(ctx)
	if err != nil {
		return res, err
	}
	if !running {
		return res, nil
	}
	indexStatus, err := r.probe(ctx, r.apiBase)
	if err != nil {
		// Up but unreachable is a real state worth reporting rather than an
		// error: the operator asked whether it is ready, and it is not.
		return res, nil
	}
	res.Ready = indexStatus.Complete
	return res, nil
}

// sortStrings sorts in place. A tiny helper so the truth-label renderer does
// not pull sort into demo.go's import set for one call.
func sortStrings(s []string) { sort.Strings(s) }
