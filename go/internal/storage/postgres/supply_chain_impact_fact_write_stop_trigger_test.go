// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"path"
	"regexp"
	"strings"
	"testing"
)

// The reject trigger (#10880 PR-C2 k, migration 349) is the database half of the
// finding-fact write stop; the C0 classifier is the reducer half. They are coupled
// by two literals the classifier reads off the real *pgconn.PgError: the SQLSTATE
// and the CONSTRAINT diagnostic. The repo's older triggers (088, 092, 250) raise
// with ERRCODE alone, which leaves the constraint name empty, so a trigger written
// in that style would make the classifier silently never fire. These tests read the
// migration text and pin the pair. The live half (a real INSERT fails with this
// error and classifies) is owed to the lab: ...fact_write_stop_live_test.go.

const (
	factWriteStopMigration = "349_supply_chain_impact_finding_fact_write_stop.sql"
	factWriteStopTrigger   = "fact_records_reject_supply_chain_impact_finding"
	factWriteStopFunction  = "reject_supply_chain_impact_finding_fact_record_write"
)

var (
	// raiseStatement captures a RAISE EXCEPTION ... ; statement of a function body.
	raiseStatement = regexp.MustCompile(`(?is)RAISE\s+EXCEPTION\s+USING(.*?);`)
	// triggerDefinition captures the CREATE TRIGGER statement up to its EXECUTE clause.
	triggerDefinition = regexp.MustCompile(`(?is)CREATE\s+TRIGGER\s+(\w+)\s+(.*?)EXECUTE\s+FUNCTION\s+(\w+)\s*\(\s*\)\s*;`)
	triggerGuard      = regexp.MustCompile(`(?is)IF\s+NOT\s+EXISTS\s*\(\s*SELECT\s+1\s+FROM\s+pg_trigger\s+WHERE\s+tgrelid\s*=\s*'fact_records'::regclass\s+AND\s+tgname\s*=\s*'(\w+)'`)
)

// factWriteStopTriggerProblems returns every way migrationSQL departs from the
// trigger contract, or nil. It checks executable text only: comment lines are
// removed first, so a description of a forbidden form cannot satisfy or trip it.
func factWriteStopTriggerProblems(migrationSQL string) []string {
	var code []string
	for _, line := range strings.Split(migrationSQL, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			code = append(code, line)
		}
	}
	body := strings.Join(code, "\n")

	var problems []string
	raise := raiseStatement.FindStringSubmatch(body)
	if raise == nil {
		return append(problems, "the function has no RAISE EXCEPTION USING statement")
	}
	for name, pattern := range map[string]string{
		"ERRCODE = '" + supplyChainImpactFactWriteRejectSQLState + "'":      `(?i)ERRCODE\s*=\s*'` + supplyChainImpactFactWriteRejectSQLState + `'`,
		"CONSTRAINT = '" + supplyChainImpactFactWriteRejectConstraint + "'": `(?i)\bCONSTRAINT\s*=\s*'` + supplyChainImpactFactWriteRejectConstraint + `'`,
		"TABLE = 'fact_records'": `(?i)\bTABLE\s*=\s*'fact_records'`,
	} {
		if !regexp.MustCompile(pattern).MatchString(raise[1]) {
			problems = append(problems, "the RAISE lacks "+name+": the classifier fires on the SQLSTATE and the constraint name together")
		}
	}
	if regexp.MustCompile(`(?is)\bIF\b`).MatchString(strings.SplitN(body, "RAISE", 2)[0][strings.Index(body, "BEGIN"):]) {
		problems = append(problems, "the raise is conditional: the trigger must be unconditional")
	}
	if regexp.MustCompile(`(?i)current_setting|set_config`).MatchString(body) {
		problems = append(problems, "a setting is read or set: no setting may bypass the stop")
	}
	definition := triggerDefinition.FindStringSubmatch(body)
	if definition == nil {
		return append(problems, "no CREATE TRIGGER ... EXECUTE FUNCTION statement")
	}
	name, clauses, function := definition[1], definition[2], definition[3]
	if name != factWriteStopTrigger {
		problems = append(problems, "the trigger is named "+name+", want "+factWriteStopTrigger)
	}
	if function != factWriteStopFunction || !strings.Contains(body, "FUNCTION "+factWriteStopFunction+"()") {
		problems = append(problems, "the trigger executes "+function+", want "+factWriteStopFunction)
	}
	if !regexp.MustCompile(`(?is)^\s*BEFORE\s+INSERT\s+OR\s+UPDATE\s+ON\s+fact_records\s+FOR\s+EACH\s+ROW\s+WHEN\s*\(\s*NEW\.fact_kind\s*=\s*'reducer_supply_chain_impact_finding'\s*\)\s*$`).MatchString(clauses) {
		problems = append(problems, "the trigger is not BEFORE INSERT OR UPDATE ON fact_records FOR EACH ROW WHEN (NEW.fact_kind = 'reducer_supply_chain_impact_finding'): "+strings.TrimSpace(clauses))
	}
	guard := triggerGuard.FindStringSubmatch(body)
	if guard == nil || guard[1] != name {
		problems = append(problems, "CREATE TRIGGER is not inside an IF NOT EXISTS pg_trigger guard for its own name: a re-apply would lock fact_records or re-enable a disabled trigger")
	}
	return problems
}

func factWriteStopMigrationSQL(t *testing.T) string {
	t.Helper()
	for _, def := range BootstrapDefinitions() {
		if path.Base(def.Path) == factWriteStopMigration {
			return def.SQL
		}
	}
	t.Fatalf("bootstrap definitions carry no %s", factWriteStopMigration)
	return ""
}

// TestFactWriteStopTriggerRaisesTheClassifiedError is the GREEN half: the shipped
// migration raises with the SQLSTATE and constraint the classifier needs, names the
// table, covers INSERT and UPDATE but not DELETE, is unconditional, and is guarded.
func TestFactWriteStopTriggerRaisesTheClassifiedError(t *testing.T) {
	t.Parallel()

	if problems := factWriteStopTriggerProblems(factWriteStopMigrationSQL(t)); len(problems) != 0 {
		t.Fatalf("migration %s departs from the trigger contract:\n- %s", factWriteStopMigration, strings.Join(problems, "\n- "))
	}
}

// TestFactWriteStopTriggerContractCatchesPlantedViolations is the RED half: each
// planted departure (copied from the shipped text with one edit) is reported, so a
// trigger written in the repo's older ERRCODE-only style, or one that is
// conditional, bypassable, unguarded, or covers DELETE, cannot pass the check.
func TestFactWriteStopTriggerContractCatchesPlantedViolations(t *testing.T) {
	t.Parallel()

	shipped := factWriteStopMigrationSQL(t)
	for name, plant := range map[string]func(string) string{
		"older repo style: ERRCODE only, no constraint name": func(s string) string {
			return strings.Replace(s, "        CONSTRAINT = '"+supplyChainImpactFactWriteRejectConstraint+"',\n", "", 1)
		},
		"constraint under the wrong name": func(s string) string {
			return strings.Replace(s, "CONSTRAINT = '"+supplyChainImpactFactWriteRejectConstraint+"'", "CONSTRAINT = 'fact_records_reject_something_else'", 1)
		},
		"wrong SQLSTATE": func(s string) string {
			return strings.Replace(s, "ERRCODE = '55000'", "ERRCODE = '23514'", 1)
		},
		"no TABLE diagnostic": func(s string) string {
			return strings.Replace(s, ",\n        TABLE = 'fact_records'", "", 1)
		},
		"conditional raise": func(s string) string {
			return strings.Replace(s, "BEGIN\n    RAISE", "BEGIN\n    IF true THEN\n    RAISE", 1)
		},
		"setting bypass": func(s string) string {
			return strings.Replace(s, "BEGIN\n", "BEGIN\n    PERFORM current_setting('eshu.bypass', true);\n", 1)
		},
		"covers DELETE": func(s string) string {
			return strings.Replace(s, "BEFORE INSERT OR UPDATE ON fact_records", "BEFORE INSERT OR UPDATE OR DELETE ON fact_records", 1)
		},
		"AFTER trigger": func(s string) string {
			return strings.Replace(s, "BEFORE INSERT OR UPDATE ON fact_records", "AFTER INSERT OR UPDATE ON fact_records", 1)
		},
		"another kind": func(s string) string {
			return strings.Replace(s, "NEW.fact_kind = 'reducer_supply_chain_impact_finding'", "NEW.fact_kind = 'reducer_container_image_identity'", 1)
		},
		"statement-level trigger": func(s string) string {
			return strings.Replace(s, "FOR EACH ROW", "FOR EACH STATEMENT", 1)
		},
		"no pg_trigger guard": func(s string) string {
			return strings.Replace(s, "tgname = '"+factWriteStopTrigger+"'", "tgname = 'some_other_trigger'", 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			planted := plant(shipped)
			if planted == shipped {
				t.Fatal("the plant did not change the migration text: the test would pass vacuously")
			}
			if problems := factWriteStopTriggerProblems(planted); len(problems) == 0 {
				t.Fatalf("the contract check accepted a planted violation (%s):\n%s", name, planted)
			}
		})
	}
}

// TestFactWriteStopTriggerConstraintMatchesTheClassifier pins the coupling from
// the other side: the literals the classifier compares are the ones the check
// above requires of the migration, so editing the classifier's constant without
// the migration (or the reverse) fails here.
func TestFactWriteStopTriggerConstraintMatchesTheClassifier(t *testing.T) {
	t.Parallel()

	if supplyChainImpactFactWriteRejectConstraint != factWriteStopTrigger {
		t.Fatalf("classifier constraint %q differs from the trigger name %q the migration creates", supplyChainImpactFactWriteRejectConstraint, factWriteStopTrigger)
	}
	if supplyChainImpactFactWriteRejectSQLState != "55000" {
		t.Fatalf("classifier SQLSTATE = %q, want 55000", supplyChainImpactFactWriteRejectSQLState)
	}
}
