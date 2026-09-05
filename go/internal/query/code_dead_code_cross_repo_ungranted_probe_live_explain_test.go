// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The EXPLAIN plumbing the plan and work guards share: running the shipped
// statement under both plan modes and rendering the values the helper
// statements take.

// crossRepoDeadCodeProbePlanMode is one of the two ways the planner can be
// asked about the probe.
//
// custom passes the values with the statement, which is what a one-shot
// EXPLAIN does. generic prepares the statement and forces a plan built without
// them, which is where pgx's statement cache puts these reads in production.
// The two disagree: the shape this walk replaced planned identically under
// custom and lost its Index Cond under generic, so a plan assertion that only
// runs the first mode proves nothing about what production executes.
type crossRepoDeadCodeProbePlanMode struct {
	name    string
	generic bool
}

var crossRepoDeadCodeProbePlanModes = []crossRepoDeadCodeProbePlanMode{
	{name: "custom plan"},
	{name: "generic plan", generic: true},
}

// crossRepoDeadCodeProbePlan runs the shipped probe under the given EXPLAIN
// prefix and plan mode, and returns the plan as text.
func crossRepoDeadCodeProbePlan(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	mode crossRepoDeadCodeProbePlanMode,
	prefix string,
	producerRepoID string,
	entityIDs []string,
	grantRepositoryIDs []string,
) string {
	t.Helper()

	statement := prefix + crossRepoDeadCodeUngrantedConsumerProbeQuery
	args := []any{
		producerRepoID,
		crossRepoDeadCodeProbeTextArray(entityIDs),
		crossRepoDeadCodeProbeTextArray(grantRepositoryIDs),
		len(entityIDs),
	}
	if mode.generic {
		statement, args = crossRepoDeadCodeProbeGenericStatement(
			ctx, t, db, prefix, producerRepoID, entityIDs, grantRepositoryIDs,
		)
	}

	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil {
		t.Fatalf("explain probe (%s): %v", mode.name, err)
	}
	defer func() { _ = rows.Close() }()

	plan := strings.Builder{}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan line: %v", err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	// A generic plan is built without the parameter values, so the producer
	// repository stays a parameter marker in the plan where a custom plan
	// inlines it as a literal. Checking that is what keeps this mode honest: a
	// refactor that quietly stopped forcing the mode would otherwise leave two
	// subtests asking the planner the same question twice.
	if mode.generic && !strings.Contains(plan.String(), "repository_id <> $1") {
		t.Fatalf("plan was not built generically -- the producer repository is not a parameter in it:\n%s", plan.String())
	}
	if !mode.generic && !strings.Contains(plan.String(), "repository_id <> 'repo-producer'::text") {
		t.Fatalf("plan was not built with the values in hand:\n%s", plan.String())
	}
	return plan.String()
}

// crossRepoDeadCodeProbeGenericStatement prepares the probe on the connection
// and forces a generic plan for it, returning an EXPLAIN of the EXECUTE with no
// bind parameters left.
//
// The values are rendered into the EXECUTE rather than bound, because under
// force_generic_plan the plan is already built without them -- which is the
// point. The test pool is pinned to one connection, so the SET, the PREPARE and
// the EXPLAIN all land on the same session; the cleanup puts both back.
func crossRepoDeadCodeProbeGenericStatement(
	ctx context.Context,
	t *testing.T,
	db *sql.DB,
	prefix string,
	producerRepoID string,
	entityIDs []string,
	grantRepositoryIDs []string,
) (string, []any) {
	t.Helper()

	name := fmt.Sprintf("cross_repo_dead_code_probe_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatalf("force a generic plan: %v", err)
	}
	// Registered before the PREPARE, not after it. The pool is pinned to one
	// connection, so a PREPARE that fails would t.Fatalf with no cleanup
	// registered and leave force_generic_plan set on that session for the rest
	// of the run. The two cleanups are separate for the same reason: a
	// DEALLOCATE of a statement the PREPARE never created is an error of its
	// own, and cleanups run last-registered-first, so this still deallocates
	// before it resets.
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanupCtx, "RESET plan_cache_mode"); err != nil {
			t.Errorf("reset plan_cache_mode: %v", err)
		}
	})
	if _, err := db.ExecContext(
		ctx,
		"PREPARE "+name+"(text, text[], text[], int) AS "+crossRepoDeadCodeUngrantedConsumerProbeQuery,
	); err != nil {
		t.Fatalf("prepare the probe: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.ExecContext(cleanupCtx, "DEALLOCATE "+name); err != nil {
			t.Errorf("deallocate the probe: %v", err)
		}
	})
	return fmt.Sprintf(
		"%sEXECUTE %s(%s, %s, %s, %d)",
		prefix,
		name,
		crossRepoDeadCodeProbeQuoteLiteral(producerRepoID),
		crossRepoDeadCodeProbeQuoteLiteral(crossRepoDeadCodeProbeTextArray(entityIDs)),
		crossRepoDeadCodeProbeQuoteLiteral(crossRepoDeadCodeProbeTextArray(grantRepositoryIDs)),
		len(entityIDs),
	), nil
}

// crossRepoDeadCodeProbeQuoteLiteral renders a SQL string literal for the
// EXECUTE above. The values are test-owned entity and repository ids, never
// caller input.
//
// The name carries the probe's prefix rather than the obvious quoteLiteral
// because this file has no build tag, so it joins every tagged build of this
// package -- including `integration`, where
// cloud_resource_runtime_digest_starvation_live_test.go already declares a
// quoteLiteral. Sharing that one would couple two unrelated live proofs through
// a build tag only one of them carries.
func crossRepoDeadCodeProbeQuoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// crossRepoDeadCodeProbeTextArray renders a Postgres text[] literal for the
// helper statements above. The probe itself binds pgarray.Array; this exists so
// the reference query and the EXPLAIN take the same values without depending on
// the encoder under test.
func crossRepoDeadCodeProbeTextArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, `"`+strings.ReplaceAll(value, `"`, `\"`)+`"`)
	}
	return "{" + strings.Join(quoted, ",") + "}"
}
