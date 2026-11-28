// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Requeue-reason guard for #11615: every reducer-stage writer that returns a
// fact_work_items row to an outstanding state must also write
// last_requeue_reason from the closed telemetry.RequeueReason vocabulary, so
// no reopen path stays unattributed. It extends the #9320 reopened_at guard
// family with the reason column, covering the four writer classes of #11615
// decision 7: unconditional pending/retrying SETs, CASE-to-pending/retrying
// assignments, reopened_at assignments, and ON CONFLICT re-drives.
//
// Production queries compose shared SET-list constants across string
// literals, so a literal-level scan alone misses them. The guard therefore
// works in three layers:
//
//  1. Literal scan: a string literal that names fact_work_items, carries a
//     requeue signal, and is not excluded (another queue table) or
//     projector-marked must contain last_requeue_reason itself.
//  2. Const/func census: every package-level string const, var, or builder
//     func whose literal parts carry a requeue signal must be self-stamped,
//     registry-covered with its resolved reason, or explicitly excluded with
//     a projector/out-of-scope justification.
//  3. Shared-clause compiler pin: ReopenSucceededReducerSetClause takes the
//     reason as a typed parameter, and every call site must pass a
//     telemetry.RequeueReason constant.

// requeueReasonUnconditionalPattern matches a SET list that assigns status =
// 'pending' or 'retrying' unconditionally: an UPDATE/ON CONFLICT SET, or a
// composed SET-list constant that starts with the assignment.
var requeueReasonUnconditionalPattern = regexp.MustCompile(`(?i)(\bSET\s+status\s*=\s*'(pending|retrying)'|^\s*status\s*=\s*'(pending|retrying)'\s*,)`)

// requeueReasonCasePattern matches a CASE branch that returns a row to
// pending or retrying.
var requeueReasonCasePattern = regexp.MustCompile(`(?i)THEN\s+'(pending|retrying)'`)

// requeueReasonStampPattern matches a reopened_at assignment.
var requeueReasonStampPattern = regexp.MustCompile(`(?i)reopened_at\s*=`)

// requeueReasonUpsertPattern matches an ON CONFLICT work-item re-drive.
var requeueReasonUpsertPattern = regexp.MustCompile(`(?i)ON CONFLICT\s*\(work_item_id\)\s*DO UPDATE`)

// requeueReasonTablePattern names the shared queue row this guard covers.
var requeueReasonTablePattern = regexp.MustCompile(`(?i)\bfact_work_items\b`)

// requeueReasonExcludedTables names tables whose own status column is not a
// reducer fact work item; their writers are out of scope.
var requeueReasonExcludedTables = regexp.MustCompile(`(?i)\b(workflow_work_items|shared_projection_intents|cross_scope_completion_events|semantic_extraction_jobs)\b`)

// requeueReasonProjectorMarker marks a literal that builds a projector-stage
// row (the INSERT stage literal). Projector rows stay NULL by design (#11615
// decision 3) and read as legacy_unclassified. The marker exempts a literal
// only when the statement proves it cannot touch reducer rows: a stage
// predicate constraining the written rows to projector
// (WHERE/AND [alias.]stage = 'projector'). A stage-conditional VALUE (a CASE
// branching on stage, as the poison liveness sweep carries for its projector
// arm) proves the opposite — the statement writes either stage — so the
// marker does not exempt it.
const requeueReasonProjectorMarker = "'projector'"

// requeueReasonProjectorPredicate matches a stage predicate constraining the
// statement's rows to projector: WHERE or AND, an optional table qualifier,
// and stage = 'projector'.
var requeueReasonProjectorPredicate = regexp.MustCompile(`(?i)(WHERE|AND)\s+(?:\w+\.)?stage\s*=\s*'projector'`)

// requeueReasonColumn is the column every in-scope writer must name.
const requeueReasonColumn = "last_requeue_reason"

// requeueWriterEntry covers one composed writer: a shared SET-list const, a
// query-building func, or the parameterized reopen clause. reason is the
// expected vocabulary value the resolved SQL must carry; parameterized marks
// the shared clause whose call sites each pass their own reason;
// reasonConst names the telemetry.RequeueReason constant a delegating func
// composer passes to the parameterized clause, verified instead of the
// literal (which the shared clause embeds at call time).
type requeueWriterEntry struct {
	reason        telemetry.RequeueReason
	parameterized bool
	reasonConst   string
	justification string
}

// requeueWriterRegistry pins every composed writer the literal scan cannot
// see whole: the reason lives in another literal or arrives as a call-site
// argument. A writer missing here (and not self-stamped) fails the census.
var requeueWriterRegistry = map[string]requeueWriterEntry{
	"waitingWakeReopenSetClause": {
		reason:        telemetry.RequeueReasonWaitingWake,
		justification: "shared SET list of all waiting wakes (#10876 family)",
	},
	"activationWakeSetClause": {
		reason:        telemetry.RequeueReasonProducerActivationSettle,
		justification: "own-gate consumer wake on relationship generation activation (#7597)",
	},
	"replayUpdateClause": {
		reason:        telemetry.RequeueReasonAdminReplay,
		reasonConst:   "telemetry.RequeueReasonAdminReplay",
		justification: "admin replay of dead-letter/failed rows, reducer arm",
	},
	"replayFailedWorkItemsTemplate": {
		reason:        telemetry.RequeueReasonRecoveryRedrive,
		reasonConst:   "telemetry.RequeueReasonRecoveryRedrive",
		justification: "recovery-plane replay of terminal rows, reducer arm",
	},
	"replayFailedWorkItemsBoundedTemplate": {
		reason:        telemetry.RequeueReasonRecoveryRedrive,
		reasonConst:   "telemetry.RequeueReasonRecoveryRedrive",
		justification: "bounded recovery-plane replay of terminal rows, reducer arm",
	},
	"ReopenSucceededReducerSetClause": {
		parameterized: true,
		justification: "shared succeeded-to-pending SET list; every composer passes its own reason",
	},
	"completionWakeReopenQueryForDomain": {
		reason:        telemetry.RequeueReasonCompletionWake,
		reasonConst:   "telemetry.RequeueReasonCompletionWake",
		justification: "cross-scope completion fanout wake, succeeded arm",
	},
	"completionWakeFlagQueryForDomain": {
		reason:        telemetry.RequeueReasonCrossScopeReplay,
		justification: "cross-scope completion fanout wake, in-flight flag arm",
	},
}

// requeueExcludedDecls names string consts, vars, or builder funcs that carry
// a requeue signal but are provably not reducer requeue writers, with the
// justification the census accepted. The four ON CONFLICT re-drives below
// are projector-only by construction: each INSERTs a projector-prefixed
// work_item_id at stage 'projector', so the PK-conflicting row the DO UPDATE
// rewrites is that same projector row, never a reducer row (#11615 decision
// 3; arbiter residual 7g closed: no reducer ON CONFLICT re-drive exists, and
// the refinalize re-enqueue goes through the recovery template instead).
var requeueExcludedDecls = map[string]string{
	"reprojectReopenedCTE":                "reconcile re-projection upsert of the 'projector_' row it just inserted",
	"healProjectorActiveGenerationQuery":  "zombie-heal upsert of the 'projector_' row it just inserted",
	"refinalizeScopeProjectionsQuery":     "refinalize re-enqueue upsert of the 'refinalize_' projector-stage row it just inserted",
	"recoverWedgedActiveGenerationsQuery": "generation-liveness re-drive upsert of the 'projector_' row it just inserted",
}

// requeueSignalReports whether text carries any requeue signal.
func requeueSignalReports(text string) bool {
	return requeueReasonUnconditionalPattern.MatchString(text) ||
		requeueReasonCasePattern.MatchString(text) ||
		requeueReasonStampPattern.MatchString(text) ||
		(requeueReasonUpsertPattern.MatchString(text) &&
			(requeueReasonUnconditionalPattern.MatchString(text) ||
				requeueReasonCasePattern.MatchString(text)))
}

// requeueLiteralInScope reports whether one string literal is a reducer
// requeue writer the guard holds to the reason rule.
func requeueLiteralInScope(text string) bool {
	if !requeueReasonTablePattern.MatchString(text) {
		return false
	}
	if requeueReasonExcludedTables.MatchString(text) {
		return false
	}
	if strings.Contains(text, requeueReasonProjectorMarker) &&
		requeueReasonProjectorPredicate.MatchString(text) {
		return false
	}
	return requeueSignalReports(text)
}

// requeueDeclSource returns the raw source text of the named top-level
// declaration, for registry checks whose reason lives in code rather than in
// a string literal.
func requeueDeclSource(fset *token.FileSet, src string, file *ast.File, name string) string {
	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.FuncDecl:
			if typed.Name.Name != name {
				continue
			}
			return src[fset.Position(typed.Pos()).Offset:fset.Position(typed.End()).Offset]
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, id := range vs.Names {
					if id.Name != name {
						continue
					}
					return src[fset.Position(typed.Pos()).Offset:fset.Position(typed.End()).Offset]
				}
			}
		}
	}
	return ""
}

// unquoteGuardLiteral returns the string value of one BasicLit.
func unquoteGuardLiteral(value string) (string, error) {
	if strings.HasPrefix(value, "`") {
		return strings.Trim(value, "`"), nil
	}
	return strconv.Unquote(value)
}

// requeueDeclLiterals collects every string literal in each top-level
// declaration of one parsed file, keyed by declaration name. A GenDecl with
// several specs shares one entry per spec name.
func requeueDeclLiterals(file *ast.File) map[string][]string {
	out := map[string][]string{}
	collect := func(name string, node ast.Node) {
		ast.Inspect(node, func(inner ast.Node) bool {
			lit, ok := inner.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, err := unquoteGuardLiteral(lit.Value)
			if err != nil {
				return true
			}
			out[name] = append(out[name], text)
			return true
		})
	}
	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.FuncDecl:
			collect(typed.Name.Name, typed)
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range vs.Names {
					collect(name.Name, vs)
				}
			}
		}
	}
	return out
}

// requeueWriterViolations returns "file decl: detail" for every in-scope
// writer that does not write last_requeue_reason.
func requeueWriterViolations(t *testing.T, files map[string]string) []string {
	t.Helper()
	var violations []string
	// flaggedTexts records file+literal texts already reported by the
	// declaration pass, so the literal pass below does not report the same
	// writer twice: it stays a backstop for literals in unflagged
	// declarations.
	flaggedTexts := map[string]bool{}
	flagDecl := func(name string, lits []string) {
		for _, lit := range lits {
			flaggedTexts[name+"\x00"+lit] = true
		}
	}
	for name, src := range files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		decls := requeueDeclLiterals(file)
		for decl, lits := range decls {
			combined := strings.Join(lits, "\n")
			if entry, ok := requeueWriterRegistry[decl]; ok {
				// Registry-covered writers are verified against the
				// declaration's full source text, because the reason for
				// a func composer lives in code (the clause call's
				// argument), not in a string literal. Registry coverage
				// applies even when the SQL carries no literal requeue
				// signal (for example a flag-only replay-debt scheduler
				// that sets no status): the registry is the claim that
				// they requeue.
				source := requeueDeclSource(fset, src, file, decl)
				if entry.parameterized {
					// The shared clause writes the column through the
					// RequeueReasonSetAssignment helper, so the column
					// string need not appear literally; the helper name
					// plus the reason type is the pin.
					if !strings.Contains(source, "RequeueReasonSetAssignment") ||
						!strings.Contains(source, "RequeueReason") {
						violations = append(violations, name+" "+decl+": parameterized clause does not route the reason through the shared assignment helper")
						flagDecl(name, lits)
					}
					continue
				}
				// The column may arrive literally, or through one of the
				// shared queuestore helpers that writes it; the reason
				// may arrive as a quoted literal, or as the
				// telemetry.RequeueReason constant whose SQLLiteral the
				// writer embeds at composition time.
				hasColumn := strings.Contains(source, requeueReasonColumn) ||
					strings.Contains(source, "RequeueReasonSetAssignment") ||
					strings.Contains(source, "RequeueReasonScheduleValue") ||
					strings.Contains(source, "RequeueReasonStageGatedValue") ||
					strings.Contains(source, "RequeueReasonUpsertValue")
				hasReason := strings.Contains(source, "'"+string(entry.reason)+"'") ||
					(entry.reasonConst != "" && strings.Contains(source, entry.reasonConst))
				if entry.reasonConst != "" && strings.Contains(entry.reasonConst, "telemetry.RequeueReason") &&
					strings.Contains(source, "ReopenSucceededReducerSetClause(") {
					// A delegating composer passes the reason constant
					// to the shared clause, which embeds the literal at
					// call time; require the constant at the clause
					// call so a bare constant reference cannot satisfy
					// the pin.
					if !strings.Contains(source, entry.reasonConst) {
						violations = append(violations, name+" "+decl+": registry wants reason constant "+
							entry.reasonConst+" routed through the shared clause")
						flagDecl(name, lits)
					}
					continue
				}
				if !hasColumn || !hasReason {
					violations = append(violations, name+" "+decl+": registry wants reason "+
						string(entry.reason)+", resolved SQL lacks the column, helper, literal, or constant")
					flagDecl(name, lits)
				}
				continue
			}
			if !requeueSignalReports(combined) {
				continue
			}
			if _, excluded := requeueExcludedDecls[decl]; excluded {
				continue
			}
			// Unregistered declaration: in scope only when it names the
			// queue table without an exclusion or an effective
			// projector marker (only a stage predicate constraining
			// the written rows to projector exempts the marker).
			if !requeueReasonTablePattern.MatchString(combined) ||
				requeueReasonExcludedTables.MatchString(combined) ||
				(strings.Contains(combined, requeueReasonProjectorMarker) &&
					requeueReasonProjectorPredicate.MatchString(combined)) {
				continue
			}
			if !strings.Contains(combined, requeueReasonColumn) {
				violations = append(violations, name+" "+decl+": requeues without "+requeueReasonColumn)
				flagDecl(name, lits)
			}
		}
	}
	// Literal-level pass: every individually in-scope literal must stamp
	// itself, unless every declaration holding that exact literal in the
	// file is registry-covered or explicitly excluded (a human classified
	// it). Composed writers split their SQL across literals, but none of
	// the registry-covered splits is individually in scope (the signal and
	// the table never meet in one unstamped fragment): any future split
	// that puts a signal and the table in one literal without the column
	// fails here until it is stamped or classified.
	for name, src := range files {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		decls := requeueDeclLiterals(file)
		holders := map[string][]string{}
		for decl, lits := range decls {
			for _, lit := range lits {
				holders[lit] = append(holders[lit], decl)
			}
		}
		classified := func(text string) bool {
			declList, ok := holders[text]
			if !ok || len(declList) == 0 {
				return false
			}
			for _, decl := range declList {
				if _, excluded := requeueExcludedDecls[decl]; excluded {
					continue
				}
				if _, covered := requeueWriterRegistry[decl]; covered {
					continue
				}
				return false
			}
			return true
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, err := unquoteGuardLiteral(lit.Value)
			if err != nil {
				return true
			}
			if flaggedTexts[name+"\x00"+text] {
				return true
			}
			if classified(text) {
				return true
			}
			if requeueLiteralInScope(text) && !strings.Contains(text, requeueReasonColumn) {
				violations = append(violations, fset.Position(lit.Pos()).String()+
					": inline writer requeues without "+requeueReasonColumn)
			}
			return true
		})
	}
	sort.Strings(violations)
	return violations
}

// TestEveryRequeueWriterSetsReason holds every reducer requeue writer to the
// #11615 rule.
func TestEveryRequeueWriterSetsReason(t *testing.T) {
	t.Parallel()
	if violations := requeueWriterViolations(t, moduleProductionSources(t)); len(violations) > 0 {
		t.Fatalf("writers return rows to outstanding without %s (#11615):\n  %s",
			requeueReasonColumn, strings.Join(violations, "\n  "))
	}
}

// TestRequeueReasonGuardBitesOnPlantedWriters proves the guard is not
// satisfied by an empty scan: planted writers of each class without a reason
// fail, their stamped twins and excluded-table writers pass.
func TestRequeueReasonGuardBitesOnPlantedWriters(t *testing.T) {
	t.Parallel()
	planted := map[string]string{
		"planted.go": "package planted\n" +
			"const unconditional = `UPDATE fact_work_items SET status = 'pending', updated_at = $1 WHERE status = 'dead_letter'`\n" +
			"const retrying = `UPDATE fact_work_items SET status = 'retrying', updated_at = $1 WHERE status = 'claimed'`\n" +
			"const cased = `UPDATE fact_work_items SET status = CASE WHEN status = 'succeeded' THEN 'pending' ELSE status END WHERE work_item_id = $2`\n" +
			"const stamped = `UPDATE fact_work_items SET status = 'pending', reopened_at = $1 WHERE status = 'succeeded'`\n" +
			"const mixedStage = `UPDATE fact_work_items AS target SET status = 'pending', attempt_count = CASE WHEN target.stage = 'projector' THEN 1 ELSE 0 END WHERE target.status = 'dead_letter'`\n",
	}
	if got := requeueWriterViolations(t, planted); len(got) != 5 {
		t.Fatalf("guard flagged %d planted writers, want 5: %v", len(got), got)
	}
	clean := map[string]string{
		"clean.go": "package clean\n" +
			"const unconditional = `UPDATE fact_work_items SET status = 'pending', last_requeue_reason = 'admin_replay', updated_at = $1 WHERE status = 'dead_letter'`\n" +
			"const retrying = `UPDATE fact_work_items SET status = 'retrying', last_requeue_reason = 'retry_backoff', updated_at = $1 WHERE status = 'claimed'`\n" +
			"const cased = `UPDATE fact_work_items SET status = CASE WHEN status = 'succeeded' THEN 'pending' ELSE status END, last_requeue_reason = CASE WHEN status = 'succeeded' THEN 'cross_scope_replay' ELSE last_requeue_reason END WHERE work_item_id = $2`\n" +
			"const stamped = `UPDATE fact_work_items SET status = 'pending', last_requeue_reason = 'maintenance_reopen', reopened_at = $1 WHERE status = 'succeeded'`\n" +
			"const workflow = `UPDATE workflow_work_items AS item SET status = 'pending' WHERE item.status = 'claimed'`\n" +
			"const projector = `UPDATE fact_work_items SET status = 'retrying', updated_at = $1 WHERE stage = 'projector' AND status = 'claimed'`\n",
	}
	if got := requeueWriterViolations(t, clean); len(got) != 0 {
		t.Fatalf("guard flagged stamped or out-of-scope writers: %v", got)
	}
}

// requeueReasonHelperNames are the shared queuestore SQL helpers that take
// a reason literal. They reference RequeueReason in their names but are not
// vocabulary members.
var requeueReasonHelperNames = map[string]bool{
	"RequeueReasonSetAssignment":   true,
	"RequeueReasonScheduleValue":   true,
	"RequeueReasonUpsertValue":     true,
	"RequeueReasonStageGatedValue": true,
	"RequeueReasonObserver":        true,
	"RequeueReasonDepth":           true,
	"RequeueReasonOldestAge":       true,
	"RequeueReasonDepths":          true,
	"RequeueReasons":               true,
	"RequeueReasonGauges":          true,
}

// requeueReasonForbiddenLiteral matches an assignment of a forbidden writer
// value to the reason column: the legacy_unclassified display label or
// 'unknown'. Both would recreate the unattributed remainder this issue
// exists to eliminate.
var requeueReasonForbiddenLiteral = regexp.MustCompile(`(?i)last_requeue_reason\s*=\s*'(legacy_unclassified|unknown)'|THEN\s+'(legacy_unclassified|unknown)'`)

// TestRequeueReasonVocabularyClosure pins the closed vocabulary at the call
// sites: every telemetry.RequeueReason constant referenced from production
// queue writers validates, the forbidden display label never appears as a
// writer value, and every registry reason is consumed by at least one
// writer.
func TestRequeueReasonVocabularyClosure(t *testing.T) {
	t.Parallel()
	files := moduleProductionSources(t)
	used := map[telemetry.RequeueReason]int{}
	namePattern := regexp.MustCompile(`RequeueReason([A-Za-z]+)`)
	for name, src := range files {
		if !strings.Contains(src, "RequeueReason") {
			continue
		}
		for _, match := range namePattern.FindAllStringSubmatch(src, -1) {
			full := "RequeueReason" + match[1]
			if requeueReasonHelperNames[full] {
				continue
			}
			var r telemetry.RequeueReason
			switch full {
			case "RequeueReasonRetryBackoff":
				r = telemetry.RequeueReasonRetryBackoff
			case "RequeueReasonReadinessWait":
				r = telemetry.RequeueReasonReadinessWait
			case "RequeueReasonWaitingWake":
				r = telemetry.RequeueReasonWaitingWake
			case "RequeueReasonMaintenanceReopen":
				r = telemetry.RequeueReasonMaintenanceReopen
			case "RequeueReasonProducerActivationSettle":
				r = telemetry.RequeueReasonProducerActivationSettle
			case "RequeueReasonCompletionWake":
				r = telemetry.RequeueReasonCompletionWake
			case "RequeueReasonCrossScopeReplay":
				r = telemetry.RequeueReasonCrossScopeReplay
			case "RequeueReasonFanoutHolder":
				r = telemetry.RequeueReasonFanoutHolder
			case "RequeueReasonRetouchNewEvidence":
				r = telemetry.RequeueReasonRetouchNewEvidence
			case "RequeueReasonAdminReplay":
				r = telemetry.RequeueReasonAdminReplay
			case "RequeueReasonAdminReopen":
				r = telemetry.RequeueReasonAdminReopen
			case "RequeueReasonRecoveryRedrive":
				r = telemetry.RequeueReasonRecoveryRedrive
			case "RequeueReasonLegacyUnclassified":
				// The display label is defined and read in
				// telemetry/requeue_reason.go only; anywhere else it is
				// a writer value and fails.
				if strings.HasSuffix(name, "internal/telemetry/requeue_reason.go") {
					continue
				}
				t.Fatalf("%s references the forbidden display label RequeueReasonLegacyUnclassified as a writer value", name)
			default:
				t.Fatalf("%s references unknown reason constant %s", name, full)
			}
			used[r]++
		}
		if loc := requeueReasonForbiddenLiteral.FindString(src); loc != "" {
			t.Fatalf("%s writes a forbidden reason value (%s)", name, loc)
		}
	}
	for _, r := range telemetry.AllRequeueReasons() {
		if used[r] == 0 {
			t.Fatalf("vocabulary member %q has no production writer; freeze discipline requires a wired writer per value", string(r))
		}
	}
	for decl, entry := range requeueWriterRegistry {
		if entry.parameterized {
			continue
		}
		if used[entry.reason] == 0 {
			t.Fatalf("registry entry %s wants reason %q with no production writer", decl, string(entry.reason))
		}
	}
}
